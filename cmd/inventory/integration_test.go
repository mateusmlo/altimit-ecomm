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
	"github.com/mateusmlo/altimit-ecomm/internal/inventory"
	"github.com/mateusmlo/altimit-ecomm/internal/kafka"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
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

	_, err = admin.CreateTopics(ctx, 1, 1, nil, "inventory.commands", "inventory.replies")
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
				Orders:       "orders",
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
			InventoryService:    "inventory-service-test",
			PaymentService:      "payment-service",
			NotificationService: "notification-service",
		},
		Region: "us-east-1",
	}
}

func seedProducts(t *testing.T, db *gorm.DB, products ...*models.Product) {
	t.Helper()
	for _, p := range products {
		require.NoError(t, db.Create(p).Error)
	}
}

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

func produceCommand(t *testing.T, ctx context.Context, brokers []string, eventType models.EventType, orderID uuid.UUID, payload []byte) {
	t.Helper()

	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	require.NoError(t, err)
	defer client.Close()

	metadata := kafka.RecordMetadata{
		EventType: eventType,
		EventID:   uuid.New(),
		SagaID:    uuid.New(),
		OrderID:   orderID,
		Timestamp: time.Now().Unix(),
	}

	metadataBytes, err := metadata.MarshalBinary()
	require.NoError(t, err)

	rec := &kgo.Record{
		Topic: "inventory.commands",
		Key:   []byte(orderID.String()),
		Value: payload,
		Headers: []kgo.RecordHeader{
			{Key: "metadata", Value: metadataBytes},
		},
	}

	err = client.ProduceSync(ctx, rec).FirstErr()
	require.NoError(t, err)
}

func consumeReply(t *testing.T, ctx context.Context, brokers []string, orderID uuid.UUID, expectedTypes ...models.EventType) (kafka.RecordMetadata, models.InventoryReply) {
	t.Helper()

	groupID := "test-reply-consumer-" + uuid.NewString()[:8]
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ConsumerGroup(groupID),
		kgo.ConsumeTopics("inventory.replies"),
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

	deadline := time.After(10 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for reply on inventory.replies for order %s", orderID)
		default:
		}

		fetchCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		fetches := client.PollFetches(fetchCtx)
		cancel()

		var metadata kafka.RecordMetadata
		var reply models.InventoryReply
		var found bool

		fetches.EachRecord(func(rec *kgo.Record) {
			if found {
				return
			}

			var m kafka.RecordMetadata
			for _, h := range rec.Headers {
				if h.Key == "metadata" {
					if err := sonic.Unmarshal(h.Value, &m); err != nil {
						return
					}
					break
				}
			}

			// Filter by orderID and optionally by event type
			if m.OrderID != orderID || !matchesType(m.EventType) {
				return
			}

			metadata = m
			require.NoError(t, sonic.Unmarshal(rec.Value, &reply))
			found = true
		})

		if found {
			return metadata, reply
		}
	}
}

func startService(ctx context.Context, t *testing.T, cfg *config.Config, db *gorm.DB) context.CancelFunc {
	t.Helper()

	svcCtx, cancel := context.WithCancel(ctx)

	producer, err := kafka.NewProducer(cfg)
	require.NoError(t, err)

	repo := repository.NewInventoryReservationRepository(db)
	service := inventory.NewInventoryService(repo)
	handler := inventory.NewHandler(service, producer, cfg)

	// Use a unique group ID per test to avoid offset conflicts
	groupID := "inventory-svc-test-" + uuid.NewString()[:8]
	consumer, err := kafka.NewConsumer(cfg, groupID, []string{cfg.Topics.Commands.Inventory})
	require.NoError(t, err)

	go func() {
		if err := consumer.Consume(svcCtx, handler.HandleCommand); err != nil && err != context.Canceled {
			log.Printf("consumer error: %v", err)
		}
		producer.Client.Close()
		consumer.Client.Close()
	}()

	return cancel
}

// --- Test Cases ---

func TestReserveInventory_Success(t *testing.T) {
	cleanupTables(t, testDB)

	product1 := &models.Product{ID: uuid.New(), Name: "Widget A", Price: 10.00, Stock: 100}
	product2 := &models.Product{ID: uuid.New(), Name: "Widget B", Price: 20.00, Stock: 50}
	seedProducts(t, testDB, product1, product2)

	ctx := context.Background()
	cancel := startService(ctx, t, testCfg, testDB)
	defer cancel()

	orderID := uuid.New()
	cmd := models.ReserveInventoryCommand{
		Products: []models.InventoryProduct{
			{ProductID: product1.ID, Quantity: 10},
			{ProductID: product2.ID, Quantity: 20},
		},
	}
	payload, err := sonic.Marshal(cmd)
	require.NoError(t, err)

	produceCommand(t, ctx, kafkaBroker, models.EventReserveInventory, orderID, payload)

	metadata, reply := consumeReply(t, ctx, kafkaBroker, orderID)

	assert.True(t, reply.Success)
	assert.Equal(t, models.EventInventoryReserved, metadata.EventType)

	// Verify stock decremented
	var p1, p2 models.Product
	require.NoError(t, testDB.First(&p1, "product_id = ?", product1.ID).Error)
	require.NoError(t, testDB.First(&p2, "product_id = ?", product2.ID).Error)
	assert.Equal(t, 90, p1.Stock)
	assert.Equal(t, 30, p2.Stock)

	// Verify reservations created
	var reservations []models.InventoryReservation
	require.NoError(t, testDB.Where("order_id = ?", orderID).Find(&reservations).Error)
	assert.Len(t, reservations, 2)
	for _, r := range reservations {
		assert.Equal(t, models.ReservationActive, r.Status)
	}
}

func TestReserveInventory_InsufficientStock(t *testing.T) {
	cleanupTables(t, testDB)

	product := &models.Product{ID: uuid.New(), Name: "Scarce Item", Price: 5.00, Stock: 5}
	seedProducts(t, testDB, product)

	ctx := context.Background()
	cancel := startService(ctx, t, testCfg, testDB)
	defer cancel()

	orderID := uuid.New()
	cmd := models.ReserveInventoryCommand{
		Products: []models.InventoryProduct{
			{ProductID: product.ID, Quantity: 10},
		},
	}
	payload, err := sonic.Marshal(cmd)
	require.NoError(t, err)

	produceCommand(t, ctx, kafkaBroker, models.EventReserveInventory, orderID, payload)

	metadata, reply := consumeReply(t, ctx, kafkaBroker, orderID)

	assert.False(t, reply.Success)
	assert.Equal(t, models.EventReserveInventoryFailed, metadata.EventType)

	// Verify stock unchanged
	var p models.Product
	require.NoError(t, testDB.First(&p, "product_id = ?", product.ID).Error)
	assert.Equal(t, 5, p.Stock)

	// Verify no reservations
	var reservations []models.InventoryReservation
	require.NoError(t, testDB.Where("order_id = ?", orderID).Find(&reservations).Error)
	assert.Empty(t, reservations)
}

func TestReserveInventory_ProductNotFound(t *testing.T) {
	cleanupTables(t, testDB)

	ctx := context.Background()
	cancel := startService(ctx, t, testCfg, testDB)
	defer cancel()

	orderID := uuid.New()
	cmd := models.ReserveInventoryCommand{
		Products: []models.InventoryProduct{
			{ProductID: uuid.New(), Quantity: 5},
		},
	}
	payload, err := sonic.Marshal(cmd)
	require.NoError(t, err)

	produceCommand(t, ctx, kafkaBroker, models.EventReserveInventory, orderID, payload)

	metadata, reply := consumeReply(t, ctx, kafkaBroker, orderID)

	assert.False(t, reply.Success)
	assert.Equal(t, models.EventReserveInventoryFailed, metadata.EventType)
}

func TestReleaseInventory_Success(t *testing.T) {
	cleanupTables(t, testDB)

	product := &models.Product{ID: uuid.New(), Name: "Releasable Item", Price: 15.00, Stock: 100}
	seedProducts(t, testDB, product)

	ctx := context.Background()
	cancel := startService(ctx, t, testCfg, testDB)
	defer cancel()

	orderID := uuid.New()
	products := []models.InventoryProduct{
		{ProductID: product.ID, Quantity: 10},
	}

	// Step 1: Reserve
	reserveCmd := models.ReserveInventoryCommand{Products: products}
	reservePayload, err := sonic.Marshal(reserveCmd)
	require.NoError(t, err)

	produceCommand(t, ctx, kafkaBroker, models.EventReserveInventory, orderID, reservePayload)
	_, reserveReply := consumeReply(t, ctx, kafkaBroker, orderID, models.EventInventoryReserved, models.EventReserveInventoryFailed)
	require.True(t, reserveReply.Success, "reserve should succeed first")

	// Step 2: Release
	releaseCmd := models.ReleaseInventoryCommand{Products: products}
	releasePayload, err := sonic.Marshal(releaseCmd)
	require.NoError(t, err)

	produceCommand(t, ctx, kafkaBroker, models.EventReleaseInventory, orderID, releasePayload)
	metadata, releaseReply := consumeReply(t, ctx, kafkaBroker, orderID, models.EventInventoryReleased, models.EventReleaseInventoryFailed)

	assert.True(t, releaseReply.Success)
	assert.Equal(t, models.EventInventoryReleased, metadata.EventType)

	// Verify stock restored
	var p models.Product
	require.NoError(t, testDB.First(&p, "product_id = ?", product.ID).Error)
	assert.Equal(t, 100, p.Stock)

	// Verify reservations marked RELEASED
	var reservations []models.InventoryReservation
	require.NoError(t, testDB.Where("order_id = ?", orderID).Find(&reservations).Error)
	assert.Len(t, reservations, 1)
	assert.Equal(t, models.ReservationReleased, reservations[0].Status)
}

func TestReleaseInventory_NoActiveReservations(t *testing.T) {
	cleanupTables(t, testDB)

	ctx := context.Background()
	cancel := startService(ctx, t, testCfg, testDB)
	defer cancel()

	orderID := uuid.New()
	cmd := models.ReleaseInventoryCommand{
		Products: []models.InventoryProduct{
			{ProductID: uuid.New(), Quantity: 5},
		},
	}
	payload, err := sonic.Marshal(cmd)
	require.NoError(t, err)

	produceCommand(t, ctx, kafkaBroker, models.EventReleaseInventory, orderID, payload)

	metadata, reply := consumeReply(t, ctx, kafkaBroker, orderID)

	assert.False(t, reply.Success)
	assert.Equal(t, models.EventReleaseInventoryFailed, metadata.EventType)
}

func TestReserveInventory_InvalidQuantity(t *testing.T) {
	cleanupTables(t, testDB)

	ctx := context.Background()
	cancel := startService(ctx, t, testCfg, testDB)
	defer cancel()

	orderID := uuid.New()
	cmd := models.ReserveInventoryCommand{
		Products: []models.InventoryProduct{
			{ProductID: uuid.New(), Quantity: 0},
		},
	}
	payload, err := sonic.Marshal(cmd)
	require.NoError(t, err)

	produceCommand(t, ctx, kafkaBroker, models.EventReserveInventory, orderID, payload)

	metadata, reply := consumeReply(t, ctx, kafkaBroker, orderID)

	assert.False(t, reply.Success)
	assert.Equal(t, models.EventReserveInventoryFailed, metadata.EventType)
}
