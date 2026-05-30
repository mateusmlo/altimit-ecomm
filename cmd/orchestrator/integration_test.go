//go:build integration

package main

import (
	"context"
	"errors"
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
	"github.com/mateusmlo/altimit-ecomm/internal/payment"
	"github.com/mateusmlo/altimit-ecomm/internal/repository"
	"github.com/mateusmlo/altimit-ecomm/internal/stripe/stripetest"
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
		"orders.dlq",
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

// startPaymentService runs a real payment.Handler against payment.commands,
// wired with a fake Stripe service. Each call uses a unique consumer group and AtEnd offset
// reset so stale records from prior tests don't drive the handler against
// truncated orders.
func startPaymentService(ctx context.Context, t *testing.T, cfg *config.Config, db *gorm.DB, fake *stripetest.FakeService) context.CancelFunc {
	t.Helper()

	svcCtx, cancel := context.WithCancel(ctx)

	producer, err := kafka.NewProducer(cfg)
	require.NoError(t, err)

	orderRepo := repository.NewOrderRepository(db)
	paymentRepo := repository.NewPaymentRepository(db)

	service := payment.NewPaymentService(fake, paymentRepo)
	handler := payment.NewHandler(service, producer, orderRepo, cfg)

	groupID := "payment-svc-test-" + uuid.NewString()[:8]
	client, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Kafka.Brokers...),
		kgo.ConsumerGroup(groupID),
		kgo.ConsumeTopics(cfg.Topics.Commands.Payment),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()),
		kgo.DisableAutoCommit(),
	)
	require.NoError(t, err)

	go func() {
		defer client.Close()
		defer producer.Client.Close()

		for {
			select {
			case <-svcCtx.Done():
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
				if err := handler.HandleCommand(svcCtx, rec); err != nil {
					log.Printf("payment handler error: %v", err)
					processErr = err
				}
			})

			if processErr == nil {
				client.CommitUncommittedOffsets(svcCtx)
			}
		}
	}()

	// Allow consumer to connect and subscribe before tests publish.
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

// consumeCommandExcluding behaves like consumeCommand but skips records whose
// EventID is in the exclude list. Used to wait for a re-sent command after an
// earlier one has already been consumed by an upstream call.
func consumeCommandExcluding(t *testing.T, ctx context.Context, brokers []string, topic string, orderID uuid.UUID, eventType models.EventType, excludeEventIDs ...uuid.UUID) (models.RecordMetadata, []byte) {
	t.Helper()

	excluded := make(map[uuid.UUID]bool, len(excludeEventIDs))
	for _, id := range excludeEventIDs {
		excluded[id] = true
	}

	groupID := "test-cmd-consumer-" + uuid.NewString()[:8]
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ConsumerGroup(groupID),
		kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	require.NoError(t, err)
	defer client.Close()

	deadline := time.After(25 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for command on %s for order %s (event %s, excluding %v)", topic, orderID, eventType, excludeEventIDs)
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

			if m.OrderID != orderID || m.EventType != eventType || excluded[m.EventID] {
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

// consumeDLQRecord polls orders.dlq until match returns true for some record,
// or the timeout expires.
func consumeDLQRecord(t *testing.T, ctx context.Context, brokers []string, timeout time.Duration, match func(*kgo.Record) bool) *kgo.Record {
	t.Helper()

	groupID := "dlq-consumer-" + uuid.NewString()[:8]
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ConsumerGroup(groupID),
		kgo.ConsumeTopics("orders.dlq"),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	require.NoError(t, err)
	defer client.Close()

	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for matching DLQ record")
		default:
		}

		fetchCtx, fetchCancel := context.WithTimeout(ctx, 2*time.Second)
		fetches := client.PollFetches(fetchCtx)
		fetchCancel()

		var found *kgo.Record
		fetches.EachRecord(func(rec *kgo.Record) {
			if found != nil {
				return
			}
			if match(rec) {
				found = rec
			}
		})
		if found != nil {
			return found
		}
	}
}

// headerValue returns the value of the first header matching key, or "".
func headerValue(rec *kgo.Record, key string) string {
	for _, h := range rec.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
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

	fake := stripetest.New()
	paymentCancel := startPaymentService(ctx, t, testCfg, testDB, fake)
	defer paymentCancel()

	publishOrderCommand(t, ctx, kafkaBroker, order)

	// Step 1: Orchestrator publishes ReserveInventory — stub the reply
	meta, _ := consumeCommand(t, ctx, kafkaBroker, "inventory.commands", order.ID, models.EventReserveInventory)
	publishReply(t, ctx, kafkaBroker, "inventory.replies", meta.SagaID, order.ID, models.EventInventoryReserved, models.InventoryReply{Success: true, Message: "reserved"})

	// Step 2: Real payment service handles ProcessPayment and publishes the reply

	// Step 3: Orchestrator publishes SendNotification — stub the reply
	meta, _ = consumeCommand(t, ctx, kafkaBroker, "notification.commands", order.ID, models.EventSendNotification)
	publishReply(t, ctx, kafkaBroker, "notification.replies", meta.SagaID, order.ID, models.EventNotificationSent, models.NotificationReply{Success: true, Message: "sent"})

	// Assert final state
	waitForSagaStatus(t, testDB, order.ID, models.SagaCompleted, 15*time.Second)
	assertOrderStatus(t, testDB, order.ID, models.OrderCompleted)

	// Real payment service should have persisted a COMPLETED payment row
	var payments []models.Payment
	require.NoError(t, testDB.Where("order_id = ?", order.ID).Find(&payments).Error)
	require.Len(t, payments, 1)
	assert.Equal(t, models.PaymentCompleted, payments[0].Status)
}

func TestSagaFailAtFirstStep_NoCompensation(t *testing.T) {
	cleanupTables(t, testDB)

	product := &models.Product{ID: uuid.New(), Name: "Widget", Price: 10.00, Stock: 100}
	seedProducts(t, testDB, product)

	order := createTestOrder(t, testDB, []*models.Product{product})

	ctx := context.Background()
	cancel := startOrchestrator(ctx, t, testCfg, testDB)
	defer cancel()

	// Payment service is started but never invoked — fails at step 1.
	fake := stripetest.New()
	paymentCancel := startPaymentService(ctx, t, testCfg, testDB, fake)
	defer paymentCancel()

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

	// Configure Stripe to decline — real payment service will publish a failed reply.
	fake := stripetest.New()
	fake.SetProcessDeclined()
	paymentCancel := startPaymentService(ctx, t, testCfg, testDB, fake)
	defer paymentCancel()

	publishOrderCommand(t, ctx, kafkaBroker, order)

	// Step 1: ReserveInventory → success
	meta, _ := consumeCommand(t, ctx, kafkaBroker, "inventory.commands", order.ID, models.EventReserveInventory)
	publishReply(t, ctx, kafkaBroker, "inventory.replies", meta.SagaID, order.ID, models.EventInventoryReserved, models.InventoryReply{Success: true, Message: "reserved"})

	// Step 2: Real payment service processes ProcessPayment, publishes PaymentFailed

	// Compensation: Orchestrator should publish ReleaseInventory
	meta, _ = consumeCommand(t, ctx, kafkaBroker, "inventory.commands", order.ID, models.EventReleaseInventory)
	publishReply(t, ctx, kafkaBroker, "inventory.replies", meta.SagaID, order.ID, models.EventInventoryReleased, models.InventoryReply{Success: true, Message: "released"})

	// Assert final state
	waitForSagaStatus(t, testDB, order.ID, models.SagaCompensated, 15*time.Second)
	assertOrderStatus(t, testDB, order.ID, models.OrderFailed)

	// Declined payment should not have persisted a payment row
	var count int64
	require.NoError(t, testDB.Model(&models.Payment{}).Where("order_id = ?", order.ID).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}

func TestSagaFailAtNotification_FullCompensationChain(t *testing.T) {
	cleanupTables(t, testDB)

	product := &models.Product{ID: uuid.New(), Name: "Widget", Price: 10.00, Stock: 100}
	seedProducts(t, testDB, product)

	order := createTestOrder(t, testDB, []*models.Product{product})

	ctx := context.Background()
	cancel := startOrchestrator(ctx, t, testCfg, testDB)
	defer cancel()

	// Default: payment succeeds on ProcessPayment and RefundPayment.
	fake := stripetest.New()
	paymentCancel := startPaymentService(ctx, t, testCfg, testDB, fake)
	defer paymentCancel()

	publishOrderCommand(t, ctx, kafkaBroker, order)

	// Step 1: ReserveInventory → success
	meta, _ := consumeCommand(t, ctx, kafkaBroker, "inventory.commands", order.ID, models.EventReserveInventory)
	publishReply(t, ctx, kafkaBroker, "inventory.replies", meta.SagaID, order.ID, models.EventInventoryReserved, models.InventoryReply{Success: true, Message: "reserved"})

	// Step 2: Real payment service handles ProcessPayment → succeeds

	// Step 3: SendNotification → failure
	meta, _ = consumeCommand(t, ctx, kafkaBroker, "notification.commands", order.ID, models.EventSendNotification)
	publishReply(t, ctx, kafkaBroker, "notification.replies", meta.SagaID, order.ID, models.EventNotificationFailed, models.NotificationReply{Success: false, Message: "email service down"})

	// Compensation step 1: Real payment service handles RefundPayment
	// Compensation step 2: ReleaseInventory — still stubbed
	meta, _ = consumeCommand(t, ctx, kafkaBroker, "inventory.commands", order.ID, models.EventReleaseInventory)
	publishReply(t, ctx, kafkaBroker, "inventory.replies", meta.SagaID, order.ID, models.EventInventoryReleased, models.InventoryReply{Success: true, Message: "released"})

	// Assert final state
	waitForSagaStatus(t, testDB, order.ID, models.SagaCompensated, 15*time.Second)
	assertOrderStatus(t, testDB, order.ID, models.OrderFailed)

	// End-to-end: real payment service created the row and then refunded it
	var payments []models.Payment
	require.NoError(t, testDB.Where("order_id = ?", order.ID).Find(&payments).Error)
	require.Len(t, payments, 1)
	assert.Equal(t, models.PaymentRefunded, payments[0].Status)
}

func TestSagaDuplicateOrderDetection(t *testing.T) {
	cleanupTables(t, testDB)

	product := &models.Product{ID: uuid.New(), Name: "Widget", Price: 10.00, Stock: 100}
	seedProducts(t, testDB, product)

	order := createTestOrder(t, testDB, []*models.Product{product})

	ctx := context.Background()
	cancel := startOrchestrator(ctx, t, testCfg, testDB)
	defer cancel()

	fake := stripetest.New()
	paymentCancel := startPaymentService(ctx, t, testCfg, testDB, fake)
	defer paymentCancel()

	// First order submission — drive it to completion
	publishOrderCommand(t, ctx, kafkaBroker, order)

	meta, _ := consumeCommand(t, ctx, kafkaBroker, "inventory.commands", order.ID, models.EventReserveInventory)
	publishReply(t, ctx, kafkaBroker, "inventory.replies", meta.SagaID, order.ID, models.EventInventoryReserved, models.InventoryReply{Success: true})

	// Real payment service handles ProcessPayment

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

	// Payment declines → triggers compensation (ReleaseInventory, which we stub as failing).
	fake := stripetest.New()
	fake.SetProcessDeclined()
	paymentCancel := startPaymentService(ctx, t, testCfg, testDB, fake)
	defer paymentCancel()

	publishOrderCommand(t, ctx, kafkaBroker, order)

	// Step 1: ReserveInventory → success
	meta, _ := consumeCommand(t, ctx, kafkaBroker, "inventory.commands", order.ID, models.EventReserveInventory)
	publishReply(t, ctx, kafkaBroker, "inventory.replies", meta.SagaID, order.ID, models.EventInventoryReserved, models.InventoryReply{Success: true, Message: "reserved"})

	// Step 2: Real payment service declines ProcessPayment → triggers compensation

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

// TestDLQ_ConsumerForwardsFailedRecord drives the production kafka.Consumer
// against a real broker: it produces records that the handler rejects and
// verifies (1) the original record is forwarded to orders.dlq with a dlq-error
// header carrying the handler error, and (2) the consumer keeps polling after
// the failure (subsequent valid records are still processed).
func TestDLQ_ConsumerForwardsFailedRecord(t *testing.T) {
	ctx := context.Background()

	// Fresh per-run source topic so concurrent or prior runs don't pollute results.
	sourceTopic := "dlq-source-" + uuid.NewString()[:8]
	adminClient, err := kgo.NewClient(kgo.SeedBrokers(kafkaBroker...))
	require.NoError(t, err)
	admin := kadm.NewClient(adminClient)
	_, err = admin.CreateTopics(ctx, 1, 1, nil, sourceTopic)
	require.NoError(t, err)
	adminClient.Close()

	// Producer doubles as the DLQ-publish client for the consumer.
	producer, err := kafka.NewProducer(testCfg)
	require.NoError(t, err)
	defer producer.Client.Close()

	groupID := "dlq-test-svc-" + uuid.NewString()[:8]
	consumer, err := kafka.NewConsumer(
		testCfg, groupID,
		[]string{sourceTopic}, testCfg.Topics.DLQ.Orders, producer.Client,
	)
	require.NoError(t, err)
	defer consumer.Client.Close()

	handlerErr := errors.New("simulated handler error")
	processed := make(chan string, 10)
	handler := func(_ context.Context, r *kgo.Record) error {
		if string(r.Key) == "fail-key" {
			return handlerErr
		}
		processed <- string(r.Key)
		return nil
	}

	consumeCtx, consumeCancel := context.WithCancel(ctx)
	defer consumeCancel()
	go consumer.Consume(consumeCtx, handler) //nolint:errcheck

	// Give the consumer time to join the group and get a partition assignment.
	time.Sleep(1 * time.Second)

	// Unique values per run let consumeDLQRecord identify our record.
	failValue := "fail-" + uuid.NewString()
	okValue := "ok-" + uuid.NewString()

	pubClient, err := kgo.NewClient(kgo.SeedBrokers(kafkaBroker...))
	require.NoError(t, err)
	defer pubClient.Close()

	require.NoError(t, pubClient.ProduceSync(ctx, &kgo.Record{
		Topic: sourceTopic, Key: []byte("fail-key"), Value: []byte(failValue),
	}).FirstErr())
	require.NoError(t, pubClient.ProduceSync(ctx, &kgo.Record{
		Topic: sourceTopic, Key: []byte("ok-key"), Value: []byte(okValue),
	}).FirstErr())

	// The "ok" record being processed proves the consumer continued past the failed one.
	select {
	case got := <-processed:
		assert.Equal(t, "ok-key", got, "consumer must continue past failed record")
	case <-time.After(20 * time.Second):
		t.Fatal("consumer stopped after first failed record (regression)")
	}

	// Verify the failed record landed in orders.dlq with the error header.
	dlqRec := consumeDLQRecord(t, ctx, kafkaBroker, 15*time.Second, func(rec *kgo.Record) bool {
		return string(rec.Value) == failValue
	})
	assert.Equal(t, []byte("fail-key"), dlqRec.Key)
	assert.Equal(t, handlerErr.Error(), headerValue(dlqRec, "dlq-error"))
}

// TestDLQ_CompensationFailure_PublishesAlert drives a saga through compensation
// retries until permanent failure and verifies the orchestrator publishes a
// CompensationFailed alert to orders.dlq.
//
// The retry worker ticks every 5 s, so to keep the test fast we shortcut one
// retry cycle by updating CompensationRetries=2 and NextRetryAt in the past
// after the first natural failure. The remaining flow — retry worker pickup,
// re-published compensation command, second failure, permanent transition,
// DLQ publish — runs through the real orchestrator code path.
func TestDLQ_CompensationFailure_PublishesAlert(t *testing.T) {
	cleanupTables(t, testDB)

	product := &models.Product{ID: uuid.New(), Name: "Widget", Price: 10.00, Stock: 100}
	seedProducts(t, testDB, product)

	order := createTestOrder(t, testDB, []*models.Product{product})

	ctx := context.Background()
	cancel := startOrchestrator(ctx, t, testCfg, testDB)
	defer cancel()

	// Payment declines → orchestrator triggers compensation (ReleaseInventory).
	fake := stripetest.New()
	fake.SetProcessDeclined()
	paymentCancel := startPaymentService(ctx, t, testCfg, testDB, fake)
	defer paymentCancel()

	publishOrderCommand(t, ctx, kafkaBroker, order)

	// Step 1: ReserveInventory → reply success.
	meta, _ := consumeCommand(t, ctx, kafkaBroker, "inventory.commands", order.ID, models.EventReserveInventory)
	publishReply(t, ctx, kafkaBroker, "inventory.replies", meta.SagaID, order.ID, models.EventInventoryReserved, models.InventoryReply{Success: true, Message: "reserved"})

	// Step 2: Real payment service declines → publishes PaymentFailed.

	// Compensation attempt 1: consume ReleaseInventory, reply with failure (bumps retries to 1).
	releaseMeta1, _ := consumeCommand(t, ctx, kafkaBroker, "inventory.commands", order.ID, models.EventReleaseInventory)
	publishReply(t, ctx, kafkaBroker, "inventory.replies", releaseMeta1.SagaID, order.ID, models.EventReleaseInventoryFailed, models.InventoryReply{Success: false, Message: "1st attempt failed"})

	// Give the orchestrator time to persist retries=1 and NextRetryAt before we shortcut.
	time.Sleep(2 * time.Second)

	// Shortcut: skip one full retry cycle by jumping to retries=2 with an
	// elapsed NextRetryAt. The retry worker will pick this up on its next tick.
	past := time.Now().UTC().Add(-1 * time.Second)
	require.NoError(t, testDB.Model(&models.SagaState{}).
		Where("order_id = ?", order.ID).
		Updates(map[string]any{
			"compensation_retries": 2,
			"next_retry_at":        past,
		}).Error)

	// Compensation attempt 2: retry worker re-sends ReleaseInventory (new EventID),
	// our reply pushes retries to 3 → permanent failure → DLQ alert.
	releaseMeta2, _ := consumeCommandExcluding(t, ctx, kafkaBroker, "inventory.commands", order.ID, models.EventReleaseInventory, releaseMeta1.EventID)
	publishReply(t, ctx, kafkaBroker, "inventory.replies", releaseMeta2.SagaID, order.ID, models.EventReleaseInventoryFailed, models.InventoryReply{Success: false, Message: "2nd attempt failed"})

	// Verify the saga reached permanent failure.
	waitForSagaStatus(t, testDB, order.ID, models.SagaCompensationFailed, 15*time.Second)
	assertOrderStatus(t, testDB, order.ID, models.OrderFailed)

	// Verify the DLQ has the compensation-failed alert for this order.
	dlqRec := consumeDLQRecord(t, ctx, kafkaBroker, 15*time.Second, func(rec *kgo.Record) bool {
		metaHdr := headerValue(rec, "metadata")
		if metaHdr == "" {
			return false
		}
		var m models.RecordMetadata
		if err := sonic.Unmarshal([]byte(metaHdr), &m); err != nil {
			return false
		}
		return m.OrderID == order.ID && m.EventType == models.EventCompensationFailed
	})

	var payload models.CompensationFailedPayload
	require.NoError(t, sonic.Unmarshal(dlqRec.Value, &payload))
	assert.Equal(t, order.ID, payload.OrderID)
	assert.Equal(t, releaseMeta1.SagaID, payload.SagaID)
	assert.Equal(t, string(models.StepCompensateInventory), payload.FailedStep)
	assert.Equal(t, orchestrator.MaxCompensationRetries, payload.Retries)
	assert.NotEmpty(t, payload.Reason)
}
