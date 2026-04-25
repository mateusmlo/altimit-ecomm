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

	_, err = admin.CreateTopics(ctx, 1, 1, nil, "payment.commands", "payment.replies")
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
			InventoryService:    "inventory-service",
			PaymentService:      "payment-service-test",
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

func seedOrder(t *testing.T, db *gorm.DB, amount float64, currency string) *models.Order {
	t.Helper()

	order := &models.Order{
		PublicID:    "ORD-" + uuid.NewString()[:8],
		CustomerID:  "customer-" + uuid.NewString()[:8],
		TotalAmount: amount,
		Currency:    currency,
		Status:      models.OrderPending,
		Items: []models.OrderItem{
			{ItemID: uuid.New(), Quantity: 1, Price: amount},
		},
	}

	require.NoError(t, db.Create(order).Error)
	return order
}

func seedPayment(t *testing.T, db *gorm.DB, orderID uuid.UUID, paymentIntentID string, amount float64) *models.Payment {
	t.Helper()

	p := &models.Payment{
		OrderID:         orderID,
		Amount:          amount,
		Currency:        "USD",
		Status:          models.PaymentCompleted,
		PaymentIntentID: paymentIntentID,
	}
	require.NoError(t, db.Create(p).Error)
	return p
}

func startService(ctx context.Context, t *testing.T, cfg *config.Config, db *gorm.DB, fake *stripetest.FakeService) context.CancelFunc {
	t.Helper()

	svcCtx, cancel := context.WithCancel(ctx)

	producer, err := kafka.NewProducer(cfg)
	require.NoError(t, err)

	orderRepo := repository.NewOrderRepository(db)
	paymentRepo := repository.NewPaymentRepository(db)

	service := payment.NewPaymentService(fake, paymentRepo)
	handler := payment.NewHandler(service, producer, orderRepo, cfg)

	// AtEnd + unique group so each test only sees records published after it
	// starts. Prior tests leave messages on the topic, and the handler errors
	// out (killing the consumer) if it processes a command for an order that
	// was already truncated.
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

	// Allow consumer to connect and subscribe before the test publishes.
	time.Sleep(500 * time.Millisecond)

	return cancel
}

func produceCommand(t *testing.T, ctx context.Context, brokers []string, eventType models.EventType, orderID uuid.UUID, payload []byte) {
	t.Helper()

	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	require.NoError(t, err)
	defer client.Close()

	metadata := models.RecordMetadata{
		EventType: eventType,
		EventID:   uuid.New(),
		SagaID:    uuid.New(),
		OrderID:   orderID,
		Timestamp: time.Now().Unix(),
	}

	metadataBytes, err := sonic.Marshal(metadata)
	require.NoError(t, err)

	rec := &kgo.Record{
		Topic: "payment.commands",
		Key:   []byte(orderID.String()),
		Value: payload,
		Headers: []kgo.RecordHeader{
			{Key: "metadata", Value: metadataBytes},
		},
	}

	err = client.ProduceSync(ctx, rec).FirstErr()
	require.NoError(t, err)
}

func consumeReply(t *testing.T, ctx context.Context, brokers []string, orderID uuid.UUID, expectedTypes ...models.EventType) (models.RecordMetadata, models.PaymentReply) {
	t.Helper()

	groupID := "test-reply-consumer-" + uuid.NewString()[:8]
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ConsumerGroup(groupID),
		kgo.ConsumeTopics("payment.replies"),
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
			t.Fatalf("timed out waiting for reply on payment.replies for order %s", orderID)
		default:
		}

		fetchCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		fetches := client.PollFetches(fetchCtx)
		cancel()

		var metadata models.RecordMetadata
		var reply models.PaymentReply
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
			require.NoError(t, sonic.Unmarshal(rec.Value, &reply))
			found = true
		})

		if found {
			return metadata, reply
		}
	}
}

// assertNoReply asserts that no reply for orderID is produced on payment.replies
// within the given timeout. Used when the handler is expected to error without
// publishing any response.
func assertNoReply(t *testing.T, ctx context.Context, brokers []string, orderID uuid.UUID, timeout time.Duration) {
	t.Helper()

	groupID := "test-noreply-consumer-" + uuid.NewString()[:8]
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ConsumerGroup(groupID),
		kgo.ConsumeTopics("payment.replies"),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	require.NoError(t, err)
	defer client.Close()

	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			return
		default:
		}

		fetchCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		fetches := client.PollFetches(fetchCtx)
		cancel()

		fetches.EachRecord(func(rec *kgo.Record) {
			for _, h := range rec.Headers {
				if h.Key != "metadata" {
					continue
				}
				var m models.RecordMetadata
				if err := sonic.Unmarshal(h.Value, &m); err != nil {
					return
				}
				if m.OrderID == orderID {
					t.Fatalf("unexpected reply for order %s: eventType=%s", orderID, m.EventType)
				}
			}
		})
	}
}

// --- Test Cases ---

func TestProcessPayment_Success(t *testing.T) {
	cleanupTables(t, testDB)

	fake := stripetest.New()
	fake.SetProcessSuccess("pi_test_success")

	ctx := context.Background()
	cancel := startService(ctx, t, testCfg, testDB, fake)
	defer cancel()

	order := seedOrder(t, testDB, 100.00, "USD")

	cmd := models.ProcessPaymentCommand{OrderID: order.ID, Amount: order.TotalAmount, Currency: order.Currency}
	payload, err := sonic.Marshal(cmd)
	require.NoError(t, err)

	produceCommand(t, ctx, kafkaBroker, models.EventProcessPayment, order.ID, payload)

	metadata, reply := consumeReply(t, ctx, kafkaBroker, order.ID)

	assert.Equal(t, models.EventPaymentSucceeded, metadata.EventType)
	assert.True(t, reply.Success)
	assert.Equal(t, "pi_test_success", reply.PaymentIntentID)

	var payments []models.Payment
	require.NoError(t, testDB.Where("order_id = ?", order.ID).Find(&payments).Error)
	require.Len(t, payments, 1)
	assert.Equal(t, models.PaymentCompleted, payments[0].Status)
	assert.InDelta(t, 100.00, payments[0].Amount, 0.001)
	assert.Equal(t, "pi_test_success", payments[0].PaymentIntentID)
}

func TestProcessPayment_StripeDeclined(t *testing.T) {
	cleanupTables(t, testDB)

	fake := stripetest.New()
	fake.SetProcessDeclined()

	ctx := context.Background()
	cancel := startService(ctx, t, testCfg, testDB, fake)
	defer cancel()

	order := seedOrder(t, testDB, 50.00, "USD")

	cmd := models.ProcessPaymentCommand{OrderID: order.ID, Amount: order.TotalAmount, Currency: order.Currency}
	payload, err := sonic.Marshal(cmd)
	require.NoError(t, err)

	produceCommand(t, ctx, kafkaBroker, models.EventProcessPayment, order.ID, payload)

	metadata, reply := consumeReply(t, ctx, kafkaBroker, order.ID)

	assert.Equal(t, models.EventPaymentFailed, metadata.EventType)
	assert.False(t, reply.Success)
	assert.Contains(t, reply.Message, "payment failed")

	var count int64
	require.NoError(t, testDB.Model(&models.Payment{}).Where("order_id = ?", order.ID).Count(&count).Error)
	assert.Equal(t, int64(0), count, "no payment row should be created on decline")
}

func TestProcessPayment_StripeError(t *testing.T) {
	cleanupTables(t, testDB)

	fake := stripetest.New()
	fake.SetProcessError(errors.New("stripe api unreachable"))

	ctx := context.Background()
	cancel := startService(ctx, t, testCfg, testDB, fake)
	defer cancel()

	order := seedOrder(t, testDB, 25.00, "USD")

	cmd := models.ProcessPaymentCommand{OrderID: order.ID, Amount: order.TotalAmount, Currency: order.Currency}
	payload, err := sonic.Marshal(cmd)
	require.NoError(t, err)

	produceCommand(t, ctx, kafkaBroker, models.EventProcessPayment, order.ID, payload)

	metadata, reply := consumeReply(t, ctx, kafkaBroker, order.ID)

	assert.Equal(t, models.EventPaymentFailed, metadata.EventType)
	assert.False(t, reply.Success)
	assert.Contains(t, reply.Message, "stripe api unreachable")

	var count int64
	require.NoError(t, testDB.Model(&models.Payment{}).Where("order_id = ?", order.ID).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}

func TestProcessPayment_OrderNotFound(t *testing.T) {
	cleanupTables(t, testDB)

	fake := stripetest.New()

	ctx := context.Background()
	cancel := startService(ctx, t, testCfg, testDB, fake)
	defer cancel()

	// No order seeded. handler.handleProcessPayment will fail on orderRepo.GetByID;
	// HandleCommand returns the error before reaching publishReply, so nothing
	// lands on payment.replies.
	orderID := uuid.New()
	cmd := models.ProcessPaymentCommand{OrderID: orderID, Amount: 10.00, Currency: "USD"}
	payload, err := sonic.Marshal(cmd)
	require.NoError(t, err)

	produceCommand(t, ctx, kafkaBroker, models.EventProcessPayment, orderID, payload)

	assertNoReply(t, ctx, kafkaBroker, orderID, 2*time.Second)

	var count int64
	require.NoError(t, testDB.Model(&models.Payment{}).Where("order_id = ?", orderID).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}

func TestRefundPayment_Success(t *testing.T) {
	cleanupTables(t, testDB)

	fake := stripetest.New()
	fake.SetRefundSuccess()

	ctx := context.Background()
	cancel := startService(ctx, t, testCfg, testDB, fake)
	defer cancel()

	order := seedOrder(t, testDB, 75.00, "USD")
	existing := seedPayment(t, testDB, order.ID, "pi_to_refund", 75.00)

	produceCommand(t, ctx, kafkaBroker, models.EventRefundPayment, order.ID, []byte(`{}`))

	metadata, reply := consumeReply(t, ctx, kafkaBroker, order.ID)

	assert.Equal(t, models.EventRefundSucceeded, metadata.EventType)
	assert.True(t, reply.Success)
	assert.Equal(t, "pi_to_refund", reply.PaymentIntentID)

	var updated models.Payment
	require.NoError(t, testDB.First(&updated, "id = ?", existing.ID).Error)
	assert.Equal(t, models.PaymentRefunded, updated.Status)
}

func TestRefundPayment_StripeFailure(t *testing.T) {
	cleanupTables(t, testDB)

	fake := stripetest.New()
	fake.SetRefundFailedStatus()

	ctx := context.Background()
	cancel := startService(ctx, t, testCfg, testDB, fake)
	defer cancel()

	order := seedOrder(t, testDB, 40.00, "USD")
	existing := seedPayment(t, testDB, order.ID, "pi_fail_refund", 40.00)

	produceCommand(t, ctx, kafkaBroker, models.EventRefundPayment, order.ID, []byte(`{}`))

	metadata, reply := consumeReply(t, ctx, kafkaBroker, order.ID)

	assert.Equal(t, models.EventRefundFailed, metadata.EventType)
	assert.False(t, reply.Success)
	assert.Contains(t, reply.Message, "refund failed")

	var unchanged models.Payment
	require.NoError(t, testDB.First(&unchanged, "id = ?", existing.ID).Error)
	assert.Equal(t, models.PaymentCompleted, unchanged.Status, "payment status should not change when refund fails")
}

func TestRefundPayment_NoPayment(t *testing.T) {
	cleanupTables(t, testDB)

	fake := stripetest.New()

	ctx := context.Background()
	cancel := startService(ctx, t, testCfg, testDB, fake)
	defer cancel()

	// No payment row → RefundPayment returns error from GetByOrderID → no reply.
	orderID := uuid.New()
	produceCommand(t, ctx, kafkaBroker, models.EventRefundPayment, orderID, []byte(`{}`))

	assertNoReply(t, ctx, kafkaBroker, orderID, 2*time.Second)
}
