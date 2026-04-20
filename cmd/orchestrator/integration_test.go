//go:build integration

package main

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	kafkaTC "github.com/testcontainers/testcontainers-go/modules/kafka"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	gormPG "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/mateusmlo/altimit-ecomm/internal/config"
	"github.com/mateusmlo/altimit-ecomm/internal/kafka"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"github.com/mateusmlo/altimit-ecomm/internal/orchestrator"
	"github.com/mateusmlo/altimit-ecomm/internal/repository"
)

var (
	testDB      *gorm.DB
	kafkaBroker []string
	testCfg     *config.Config
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	// 1. Start Postgres testcontainer
	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("altimit_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		log.Fatalf("failed to start postgres container: %s", err)
	}

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Fatalf("failed to get connection string: %s", err)
	}

	testDB, err = gorm.Open(gormPG.Open(connStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
	if err != nil {
		log.Fatalf("failed to connect to test database: %s", err)
	}

	// Create enum types
	enums := []string{
		`DO $$ BEGIN CREATE TYPE order_status AS ENUM ('PENDING','PROCESSING','COMPLETED','CANCELLED','FAILED'); EXCEPTION WHEN duplicate_object THEN null; END $$`,
		`DO $$ BEGIN CREATE TYPE payment_status AS ENUM ('PENDING','PROCESSING','COMPLETED','FAILED','REFUNDED'); EXCEPTION WHEN duplicate_object THEN null; END $$`,
		`DO $$ BEGIN CREATE TYPE saga_status AS ENUM ('STARTED','COMPLETED','IN_PROGRESS','CANCELLED','FAILED','COMPENSATED','COMPENSATING','COMPENSATION_FAILED'); EXCEPTION WHEN duplicate_object THEN null; END $$`,
		`DO $$ BEGIN CREATE TYPE reservation_status AS ENUM ('ACTIVE','RELEASED','CONFIRMED'); EXCEPTION WHEN duplicate_object THEN null; END $$`,
	}
	for _, sql := range enums {
		if err := testDB.Exec(sql).Error; err != nil {
			log.Fatalf("failed to create enum type: %s", err)
		}
	}

	err = testDB.AutoMigrate(
		&models.Order{},
		&models.OrderItem{},
		&models.Product{},
		&models.InventoryReservation{},
		&models.Payment{},
		&models.SagaState{},
		&models.IdempotencyKey{},
	)
	if err != nil {
		log.Fatalf("failed to auto migrate: %s", err)
	}

	// 2. Start Kafka testcontainer
	kafkaContainer, err := kafkaTC.Run(ctx,
		"confluentinc/confluent-local:7.4.0",
		kafkaTC.WithClusterID("test-cluster"),
	)
	if err != nil {
		log.Fatalf("failed to start kafka container: %s", err)
	}

	kafkaBroker, err = kafkaContainer.Brokers(ctx)
	if err != nil {
		log.Fatalf("failed to get kafka brokers: %s", err)
	}

	// 3. Create topics
	adminClient, err := kgo.NewClient(kgo.SeedBrokers(kafkaBroker...))
	if err != nil {
		log.Fatalf("failed to create admin kafka client: %s", err)
	}
	admin := kadm.NewClient(adminClient)

	topics := []string{
		"orders.commands",
		"inventory.commands", "inventory.replies",
		"payment.commands", "payment.replies",
		"notification.commands", "notification.replies",
	}
	_, err = admin.CreateTopics(ctx, 1, 1, nil, topics...)
	if err != nil {
		log.Fatalf("failed to create topics: %s", err)
	}
	adminClient.Close()

	// 4. Build test config
	testCfg = buildTestConfig(connStr, kafkaBroker)

	// 5. Run tests
	code := m.Run()

	// 6. Cleanup
	if err := kafkaContainer.Terminate(ctx); err != nil {
		log.Printf("failed to terminate kafka container: %s", err)
	}
	if err := pgContainer.Terminate(ctx); err != nil {
		log.Printf("failed to terminate postgres container: %s", err)
	}

	os.Exit(code)
}

func buildTestConfig(pgConnStr string, brokers []string) *config.Config {
	return &config.Config{
		Kafka: config.KafkaConfig{
			Brokers:           brokers,
			MaxRequestRetries: 3,
			MaxRecordRetries:  3,
		},
		Postgres: config.PostgresConfig{
			User:     "test",
			Password: "test",
			DB:       "altimit_test",
			Host:     "localhost",
			Port:     5432,
		},
		Redis: config.RedisConfig{
			Host: "localhost",
			Port: 6379,
		},
		Topics: config.TopicsConfig{
			Commands: config.CommandTopics{
				Orders:       "orders.commands",
				Inventory:    "inventory.commands",
				Payment:      "payment.commands",
				Notification: "notification.commands",
			},
			Replies: config.ReplyTopics{
				Inventory:    "inventory.replies",
				Payment:      "payment.replies",
				Notification: "notification.replies",
			},
			DLQ: config.DLQTopics{
				Orders: "orders.dlq",
			},
		},
		ConsumerGroups: config.ConsumerGroupsConfig{
			SagaOrchestrator:    "saga-orchestrator",
			InventoryService:    "inventory-service",
			PaymentService:      "payment-service",
			NotificationService: "notification-service",
		},
		Region: "us-east-1",
	}
}

// --- Helpers ---

func cleanupTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	tables := []string{
		"inventory_reservations",
		"order_items",
		"payments",
		"saga_states",
		"idempotency_keys",
		"orders",
		"products",
	}
	for _, table := range tables {
		if err := db.Exec("TRUNCATE TABLE " + table + " CASCADE").Error; err != nil {
			t.Fatalf("failed to truncate table %s: %s", table, err)
		}
	}
}

func seedProducts(t *testing.T, db *gorm.DB, products ...*models.Product) {
	t.Helper()
	for _, p := range products {
		require.NoError(t, db.Create(p).Error)
	}
}

// createTestOrder seeds an order with items in the DB and returns it with the DB-assigned ID.
// The orchestrator's handleNewOrder calls orderRepo.UpdateStatus(order.ID, ...) so
// the order must exist before the orchestrator processes the Kafka record.
func createTestOrder(t *testing.T, db *gorm.DB, products []*models.Product) *models.Order {
	t.Helper()

	var items []models.OrderItem
	totalAmount := 0.0
	for _, p := range products {
		qty := 2
		items = append(items, models.OrderItem{
			ItemID:   p.ID,
			Quantity: qty,
			Price:    p.Price,
		})
		totalAmount += p.Price * float64(qty)
	}

	order := &models.Order{
		PublicID:    "ORD-" + uuid.NewString()[:8],
		CustomerID:  "customer-" + uuid.NewString()[:8],
		TotalAmount: totalAmount,
		Status:      models.OrderPending,
		Items:       items,
	}

	require.NoError(t, db.Create(order).Error)

	// Reload with items to get DB-assigned IDs
	var loaded models.Order
	require.NoError(t, db.Preload("Items").First(&loaded, "id = ?", order.ID).Error)

	return &loaded
}

func startOrchestrator(ctx context.Context, t *testing.T, cfg *config.Config, db *gorm.DB) context.CancelFunc {
	t.Helper()

	svcCtx, cancel := context.WithCancel(ctx)

	producer, err := kafka.NewProducer(cfg)
	require.NoError(t, err)

	sagaRepo := repository.NewSagaRepository(db)
	orderRepo := repository.NewOrderRepository(db)

	orch := orchestrator.NewOrchestrator(sagaRepo, orderRepo, producer, cfg)
	handler := orchestrator.NewHandler(orch, orderRepo, sagaRepo, cfg)

	listenTopics := []string{
		cfg.Topics.Commands.Orders,
		cfg.Topics.Replies.Inventory,
		cfg.Topics.Replies.Payment,
		cfg.Topics.Replies.Notification,
	}

	// Use AtEnd offset so each test only sees records published after it starts,
	// avoiding interference from prior tests' leftover records.
	groupID := "orchestrator-test-" + uuid.NewString()[:8]
	client, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Kafka.Brokers...),
		kgo.ConsumerGroup(groupID),
		kgo.ConsumeTopics(listenTopics...),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()),
		kgo.DisableAutoCommit(),
	)
	require.NoError(t, err)

	// Start RetryWorker (matches production main.go)
	go orch.RetryWorker(svcCtx)

	go func() {
		for {
			select {
			case <-svcCtx.Done():
				client.Close()
				producer.Client.Close()
				return
			default:
			}

			fetches := client.PollFetches(svcCtx)
			if fetches.IsClientClosed() {
				return
			}

			var processErr error
			fetches.EachRecord(func(rec *kgo.Record) {
				if processErr != nil {
					return
				}
				if err := handler.HandleRecord(svcCtx, rec); err != nil {
					log.Printf("orchestrator error: %v", err)
					processErr = err
				}
			})

			if processErr == nil {
				client.CommitUncommittedOffsets(svcCtx)
			}
		}
	}()

	// Allow consumer to connect and subscribe before publishing
	time.Sleep(500 * time.Millisecond)

	return cancel
}

func publishOrderCommand(t *testing.T, ctx context.Context, brokers []string, order *models.Order) {
	t.Helper()

	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	require.NoError(t, err)
	defer client.Close()

	payload, err := sonic.Marshal(order)
	require.NoError(t, err)

	rec := &kgo.Record{
		Topic: "orders.commands",
		Key:   []byte(order.ID.String()),
		Value: payload,
	}

	err = client.ProduceSync(ctx, rec).FirstErr()
	require.NoError(t, err)
}

// consumeCommand consumes from a command topic, filtering by orderID and optionally by event type.
// Returns the metadata (containing SagaID needed for reply headers) and the raw payload.
func consumeCommand(t *testing.T, ctx context.Context, brokers []string, topic string, orderID uuid.UUID, expectedTypes ...models.EventType) (models.RecordMetadata, []byte) {
	t.Helper()

	groupID := "test-cmd-consumer-" + uuid.NewString()[:8]
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ConsumerGroup(groupID),
		kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	require.NoError(t, err)
	defer client.Close()

	matchesType := func(et models.EventType) bool {
		if len(expectedTypes) == 0 {
			return true
		}
		for _, expected := range expectedTypes {
			if et == expected {
				return true
			}
		}
		return false
	}

	deadline := time.After(15 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for command on %s for order %s (expected types: %v)", topic, orderID, expectedTypes)
		default:
		}

		fetchCtx, fetchCancel := context.WithTimeout(ctx, 2*time.Second)
		fetches := client.PollFetches(fetchCtx)
		fetchCancel()

		var metadata models.RecordMetadata
		var payload []byte
		var found bool

		fetches.EachRecord(func(rec *kgo.Record) {
			if found {
				return
			}

			var m models.RecordMetadata
			for _, h := range rec.Headers {
				if h.Key == "metadata" {
					if err := sonic.Unmarshal(h.Value, &m); err != nil {
						return
					}
					break
				}
			}

			if m.OrderID != orderID || !matchesType(m.EventType) {
				return
			}

			metadata = m
			payload = rec.Value
			found = true
		})

		if found {
			return metadata, payload
		}
	}
}

// publishReply publishes a reply to a reply topic with proper metadata headers.
func publishReply(t *testing.T, ctx context.Context, brokers []string, topic string, sagaID, orderID uuid.UUID, eventType models.EventType, reply any) {
	t.Helper()

	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	require.NoError(t, err)
	defer client.Close()

	replyPayload, err := sonic.Marshal(reply)
	require.NoError(t, err)

	metadata := models.RecordMetadata{
		EventType: eventType,
		EventID:   uuid.New(),
		SagaID:    sagaID,
		OrderID:   orderID,
		Timestamp: time.Now().Unix(),
	}

	metadataBytes, err := sonic.Marshal(metadata)
	require.NoError(t, err)

	rec := &kgo.Record{
		Topic: topic,
		Key:   []byte(orderID.String()),
		Value: replyPayload,
		Headers: []kgo.RecordHeader{
			{Key: "metadata", Value: metadataBytes},
		},
	}

	err = client.ProduceSync(ctx, rec).FirstErr()
	require.NoError(t, err)
}

// waitForSagaStatus polls the DB until the saga for the given orderID reaches the expected status.
func waitForSagaStatus(t *testing.T, db *gorm.DB, orderID uuid.UUID, expectedStatus models.SagaStatus, timeout time.Duration) *models.SagaState {
	t.Helper()

	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for saga status %s for order %s", expectedStatus, orderID)
		default:
		}

		var saga models.SagaState
		err := db.Where("order_id = ?", orderID).First(&saga).Error
		if err == nil && saga.Status == expectedStatus {
			return &saga
		}

		time.Sleep(200 * time.Millisecond)
	}
}

func assertOrderStatus(t *testing.T, db *gorm.DB, orderID uuid.UUID, expectedStatus models.OrderStatus) {
	t.Helper()

	var order models.Order
	require.NoError(t, db.First(&order, "id = ?", orderID).Error)
	assert.Equal(t, expectedStatus, order.Status)
}

// --- Test Cases ---

func TestSagaHappyPath(t *testing.T) {
	cleanupTables(t, testDB)

	product1 := &models.Product{ID: uuid.New(), Name: "Widget A", Price: 10.00, Stock: 100}
	product2 := &models.Product{ID: uuid.New(), Name: "Widget B", Price: 20.00, Stock: 50}
	seedProducts(t, testDB, product1, product2)

	order := createTestOrder(t, testDB, []*models.Product{product1, product2})

	ctx := context.Background()
	cancel := startOrchestrator(ctx, t, testCfg, testDB)
	defer cancel()

	publishOrderCommand(t, ctx, kafkaBroker, order)

	// Step 1: Orchestrator should publish ReserveInventory command
	meta, _ := consumeCommand(t, ctx, kafkaBroker, "inventory.commands", order.ID, models.EventReserveInventory)
	publishReply(t, ctx, kafkaBroker, "inventory.replies", meta.SagaID, order.ID, models.EventInventoryReserved, models.InventoryReply{Success: true, Message: "reserved"})

	// Step 2: Orchestrator should publish ProcessPayment command
	meta, _ = consumeCommand(t, ctx, kafkaBroker, "payment.commands", order.ID, models.EventProcessPayment)
	publishReply(t, ctx, kafkaBroker, "payment.replies", meta.SagaID, order.ID, models.EventPaymentSucceeded, models.PaymentReply{Success: true, PaymentIntentID: uuid.NewString(), Message: "paid"})

	// Step 3: Orchestrator should publish SendNotification command
	meta, _ = consumeCommand(t, ctx, kafkaBroker, "notification.commands", order.ID, models.EventSendNotification)
	publishReply(t, ctx, kafkaBroker, "notification.replies", meta.SagaID, order.ID, models.EventNotificationSent, models.NotificationReply{Success: true, Message: "sent"})

	// Assert final state
	waitForSagaStatus(t, testDB, order.ID, models.SagaCompleted, 15*time.Second)
	assertOrderStatus(t, testDB, order.ID, models.OrderCompleted)
}

func TestSagaFailAtFirstStep_NoCompensation(t *testing.T) {
	cleanupTables(t, testDB)

	product := &models.Product{ID: uuid.New(), Name: "Widget", Price: 10.00, Stock: 100}
	seedProducts(t, testDB, product)

	order := createTestOrder(t, testDB, []*models.Product{product})

	ctx := context.Background()
	cancel := startOrchestrator(ctx, t, testCfg, testDB)
	defer cancel()

	publishOrderCommand(t, ctx, kafkaBroker, order)

	// Step 1: Orchestrator publishes ReserveInventory → reply with failure
	meta, _ := consumeCommand(t, ctx, kafkaBroker, "inventory.commands", order.ID, models.EventReserveInventory)
	publishReply(t, ctx, kafkaBroker, "inventory.replies", meta.SagaID, order.ID, models.EventReserveInventoryFailed, models.InventoryReply{Success: false, Message: "insufficient stock"})

	// No compensation — first step has no compensation defined
	waitForSagaStatus(t, testDB, order.ID, models.SagaFailed, 15*time.Second)
	assertOrderStatus(t, testDB, order.ID, models.OrderFailed)
}

func TestSagaFailAtPayment_CompensateInventory(t *testing.T) {
	cleanupTables(t, testDB)

	product := &models.Product{ID: uuid.New(), Name: "Widget", Price: 10.00, Stock: 100}
	seedProducts(t, testDB, product)

	order := createTestOrder(t, testDB, []*models.Product{product})

	ctx := context.Background()
	cancel := startOrchestrator(ctx, t, testCfg, testDB)
	defer cancel()

	publishOrderCommand(t, ctx, kafkaBroker, order)

	// Step 1: ReserveInventory → success
	meta, _ := consumeCommand(t, ctx, kafkaBroker, "inventory.commands", order.ID, models.EventReserveInventory)
	publishReply(t, ctx, kafkaBroker, "inventory.replies", meta.SagaID, order.ID, models.EventInventoryReserved, models.InventoryReply{Success: true, Message: "reserved"})

	// Step 2: ProcessPayment → failure
	meta, _ = consumeCommand(t, ctx, kafkaBroker, "payment.commands", order.ID, models.EventProcessPayment)
	publishReply(t, ctx, kafkaBroker, "payment.replies", meta.SagaID, order.ID, models.EventPaymentFailed, models.PaymentReply{Success: false, Message: "card declined"})

	// Compensation: Orchestrator should publish ReleaseInventory
	meta, _ = consumeCommand(t, ctx, kafkaBroker, "inventory.commands", order.ID, models.EventReleaseInventory)
	publishReply(t, ctx, kafkaBroker, "inventory.replies", meta.SagaID, order.ID, models.EventInventoryReleased, models.InventoryReply{Success: true, Message: "released"})

	// Assert final state
	waitForSagaStatus(t, testDB, order.ID, models.SagaCompensated, 15*time.Second)
	assertOrderStatus(t, testDB, order.ID, models.OrderFailed)
}

func TestSagaFailAtNotification_FullCompensationChain(t *testing.T) {
	cleanupTables(t, testDB)

	product := &models.Product{ID: uuid.New(), Name: "Widget", Price: 10.00, Stock: 100}
	seedProducts(t, testDB, product)

	order := createTestOrder(t, testDB, []*models.Product{product})

	ctx := context.Background()
	cancel := startOrchestrator(ctx, t, testCfg, testDB)
	defer cancel()

	publishOrderCommand(t, ctx, kafkaBroker, order)

	// Step 1: ReserveInventory → success
	meta, _ := consumeCommand(t, ctx, kafkaBroker, "inventory.commands", order.ID, models.EventReserveInventory)
	publishReply(t, ctx, kafkaBroker, "inventory.replies", meta.SagaID, order.ID, models.EventInventoryReserved, models.InventoryReply{Success: true, Message: "reserved"})

	// Step 2: ProcessPayment → success
	meta, _ = consumeCommand(t, ctx, kafkaBroker, "payment.commands", order.ID, models.EventProcessPayment)
	publishReply(t, ctx, kafkaBroker, "payment.replies", meta.SagaID, order.ID, models.EventPaymentSucceeded, models.PaymentReply{Success: true, PaymentIntentID: uuid.NewString(), Message: "paid"})

	// Step 3: SendNotification → failure
	meta, _ = consumeCommand(t, ctx, kafkaBroker, "notification.commands", order.ID, models.EventSendNotification)
	publishReply(t, ctx, kafkaBroker, "notification.replies", meta.SagaID, order.ID, models.EventNotificationFailed, models.NotificationReply{Success: false, Message: "email service down"})

	// Compensation step 1: RefundPayment
	meta, _ = consumeCommand(t, ctx, kafkaBroker, "payment.commands", order.ID, models.EventRefundPayment)
	publishReply(t, ctx, kafkaBroker, "payment.replies", meta.SagaID, order.ID, models.EventPaymentSucceeded, models.PaymentReply{Success: true, Message: "refunded"})

	// Compensation step 2: ReleaseInventory
	meta, _ = consumeCommand(t, ctx, kafkaBroker, "inventory.commands", order.ID, models.EventReleaseInventory)
	publishReply(t, ctx, kafkaBroker, "inventory.replies", meta.SagaID, order.ID, models.EventInventoryReleased, models.InventoryReply{Success: true, Message: "released"})

	// Assert final state
	waitForSagaStatus(t, testDB, order.ID, models.SagaCompensated, 15*time.Second)
	assertOrderStatus(t, testDB, order.ID, models.OrderFailed)
}

func TestSagaDuplicateOrderDetection(t *testing.T) {
	cleanupTables(t, testDB)

	product := &models.Product{ID: uuid.New(), Name: "Widget", Price: 10.00, Stock: 100}
	seedProducts(t, testDB, product)

	order := createTestOrder(t, testDB, []*models.Product{product})

	ctx := context.Background()
	cancel := startOrchestrator(ctx, t, testCfg, testDB)
	defer cancel()

	// First order submission — drive it to completion
	publishOrderCommand(t, ctx, kafkaBroker, order)

	meta, _ := consumeCommand(t, ctx, kafkaBroker, "inventory.commands", order.ID, models.EventReserveInventory)
	publishReply(t, ctx, kafkaBroker, "inventory.replies", meta.SagaID, order.ID, models.EventInventoryReserved, models.InventoryReply{Success: true})

	meta, _ = consumeCommand(t, ctx, kafkaBroker, "payment.commands", order.ID, models.EventProcessPayment)
	publishReply(t, ctx, kafkaBroker, "payment.replies", meta.SagaID, order.ID, models.EventPaymentSucceeded, models.PaymentReply{Success: true, PaymentIntentID: uuid.NewString()})

	meta, _ = consumeCommand(t, ctx, kafkaBroker, "notification.commands", order.ID, models.EventSendNotification)
	publishReply(t, ctx, kafkaBroker, "notification.replies", meta.SagaID, order.ID, models.EventNotificationSent, models.NotificationReply{Success: true})

	waitForSagaStatus(t, testDB, order.ID, models.SagaCompleted, 15*time.Second)

	// Submit the same order again
	publishOrderCommand(t, ctx, kafkaBroker, order)

	// Wait a bit and verify no second saga was created
	time.Sleep(3 * time.Second)

	var count int64
	require.NoError(t, testDB.Model(&models.SagaState{}).Where("order_id = ?", order.ID).Count(&count).Error)
	assert.Equal(t, int64(1), count, "duplicate order should not create a second saga")
}

func TestSagaCompensationFailure_SetsRetryState(t *testing.T) {
	cleanupTables(t, testDB)

	product := &models.Product{ID: uuid.New(), Name: "Widget", Price: 10.00, Stock: 100}
	seedProducts(t, testDB, product)

	order := createTestOrder(t, testDB, []*models.Product{product})

	testStart := time.Now()

	ctx := context.Background()
	cancel := startOrchestrator(ctx, t, testCfg, testDB)
	defer cancel()

	publishOrderCommand(t, ctx, kafkaBroker, order)

	// Step 1: ReserveInventory → success
	meta, _ := consumeCommand(t, ctx, kafkaBroker, "inventory.commands", order.ID, models.EventReserveInventory)
	publishReply(t, ctx, kafkaBroker, "inventory.replies", meta.SagaID, order.ID, models.EventInventoryReserved, models.InventoryReply{Success: true, Message: "reserved"})

	// Step 2: ProcessPayment → failure (triggers compensation)
	meta, _ = consumeCommand(t, ctx, kafkaBroker, "payment.commands", order.ID, models.EventProcessPayment)
	publishReply(t, ctx, kafkaBroker, "payment.replies", meta.SagaID, order.ID, models.EventPaymentFailed, models.PaymentReply{Success: false, Message: "card declined"})

	// Compensation: ReleaseInventory → also fails
	meta, _ = consumeCommand(t, ctx, kafkaBroker, "inventory.commands", order.ID, models.EventReleaseInventory)
	publishReply(t, ctx, kafkaBroker, "inventory.replies", meta.SagaID, order.ID, models.EventReleaseInventoryFailed, models.InventoryReply{Success: false, Message: "inventory service unavailable"})

	// Wait for the orchestrator to process the compensation failure
	time.Sleep(2 * time.Second)

	// Verify retry state
	var saga models.SagaState
	require.NoError(t, testDB.Where("order_id = ?", order.ID).First(&saga).Error)

	assert.Equal(t, models.SagaCompensating, saga.Status, "saga should remain in COMPENSATING status for retry")
	assert.Equal(t, 1, saga.CompensationRetries, "compensation retries should be 1")
	assert.NotNil(t, saga.NextRetryAt, "next retry time should be set")
	assert.True(t, saga.NextRetryAt.After(testStart), "next retry should be scheduled after test began")
}
