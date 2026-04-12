// test-saga simulates a full saga lifecycle against a running orchestrator + inventory service.
// It publishes an order to the orders topic, then acts as a mock payment and notification
// service: consuming commands from Kafka and replying with success.
//
// Prerequisites:
//
//	make start && make db-seed && make run-inventory  (in one terminal)
//	make run-orchestrator                              (in another terminal)
//
// Then: make test-saga
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/mateusmlo/altimit-ecomm/internal/config"
	"github.com/mateusmlo/altimit-ecomm/internal/kafka"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"github.com/twmb/franz-go/pkg/kgo"
	gormPG "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	db, err := gorm.Open(gormPG.Open(cfg.GetPostgresConnectionString()), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Load seeded products
	var products []models.Product
	if err := db.Find(&products).Error; err != nil || len(products) == 0 {
		log.Fatal("No products found — run `make db-seed` first")
	}

	fmt.Println("========================================")
	fmt.Println("  Saga Live Test")
	fmt.Println("========================================")
	fmt.Printf("Products in DB: %d\n\n", len(products))

	// 2. Create an order in Postgres
	order := createOrder(db, products[:2])
	fmt.Printf("[order] Created %s  (ID: %s)\n", order.PublicID, order.ID)

	// 3. Publish order to the orders topic
	publishOrder(ctx, cfg, order)
	fmt.Printf("[kafka] Published order to topic %q\n\n", cfg.Topics.Commands.Orders)

	// 4. Spin up a consumer for command topics we need to reply to
	//    The real inventory service handles inventory.commands.
	//    We simulate payment + notification services.
	commandTopics := []string{
		cfg.Topics.Commands.Payment,
		cfg.Topics.Commands.Notification,
	}

	groupID := "test-saga-" + uuid.NewString()[:8]
	client, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Kafka.Brokers...),
		kgo.ConsumerGroup(groupID),
		kgo.ConsumeTopics(commandTopics...),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()),
	)
	if err != nil {
		log.Fatalf("Failed to create consumer: %v", err)
	}
	defer client.Close()

	// Give the consumer group time to stabilise
	time.Sleep(500 * time.Millisecond)

	replier, err := kgo.NewClient(kgo.SeedBrokers(cfg.Kafka.Brokers...))
	if err != nil {
		log.Fatalf("Failed to create replier client: %v", err)
	}
	defer replier.Close()

	// 5. Loop: consume commands, reply automatically
	replied := map[string]bool{}
	for {
		select {
		case <-ctx.Done():
			printResult(db, order.ID)
			return
		default:
		}

		fetchCtx, fetchCancel := context.WithTimeout(ctx, 2*time.Second)
		fetches := client.PollFetches(fetchCtx)
		fetchCancel()

		fetches.EachRecord(func(rec *kgo.Record) {
			meta := extractMetadata(rec)
			if meta == nil || meta.OrderID != order.ID {
				return
			}

			key := fmt.Sprintf("%s:%s", rec.Topic, meta.EventType)
			if replied[key] {
				return
			}

			switch {
			case rec.Topic == cfg.Topics.Commands.Payment && meta.EventType == models.EventProcessPayment:
				fmt.Printf("[payment]      << received PROCESS_PAYMENT command\n")
				sendReply(ctx, replier, cfg.Topics.Replies.Payment, meta, models.EventPaymentSucceeded,
					&models.PaymentReply{Success: true, PaymentIntentID: uuid.NewString(), Message: "charged"})
				fmt.Printf("[payment]      >> replied PAYMENT_PROCESSED\n")

			case rec.Topic == cfg.Topics.Commands.Payment && meta.EventType == models.EventRefundPayment:
				fmt.Printf("[payment]      << received REFUND_PAYMENT command\n")
				sendReply(ctx, replier, cfg.Topics.Replies.Payment, meta, models.EventPaymentSucceeded,
					&models.PaymentReply{Success: true, Message: "refunded"})
				fmt.Printf("[payment]      >> replied PAYMENT_PROCESSED (refund)\n")

			case rec.Topic == cfg.Topics.Commands.Notification && meta.EventType == models.EventSendNotification:
				fmt.Printf("[notification] << received SEND_NOTIFICATION command\n")
				sendReply(ctx, replier, cfg.Topics.Replies.Notification, meta, models.EventNotificationSent,
					&models.NotificationReply{Success: true, Message: "email sent"})
				fmt.Printf("[notification] >> replied NOTIFICATION_SENT\n")

			default:
				fmt.Printf("[???]          << unknown command on %s: %s\n", rec.Topic, meta.EventType)
				return
			}

			replied[key] = true
		})

		// Check if saga completed
		var saga models.SagaState
		if err := db.Where("order_id = ?", order.ID).First(&saga).Error; err == nil {
			if saga.Status == models.SagaCompleted || saga.Status == models.SagaFailed ||
				saga.Status == models.SagaCompensated || saga.Status == models.SagaCompensationFailed {
				fmt.Println()
				printResult(db, order.ID)
				return
			}
		}
	}
}

func createOrder(db *gorm.DB, products []models.Product) *models.Order {
	var items []models.OrderItem
	total := 0.0
	for _, p := range products {
		qty := 1
		items = append(items, models.OrderItem{
			ItemID:   p.ID,
			Quantity: qty,
			Price:    p.Price,
		})
		total += p.Price * float64(qty)
	}

	order := &models.Order{
		PublicID:    "ORD-" + uuid.NewString()[:8],
		CustomerID:  "cust-live-test",
		TotalAmount: total,
		Status:      models.OrderPending,
		Items:       items,
	}

	if err := db.Create(order).Error; err != nil {
		log.Fatalf("Failed to create order: %v", err)
	}

	// Reload with items for proper IDs
	var loaded models.Order
	if err := db.Preload("Items").First(&loaded, "id = ?", order.ID).Error; err != nil {
		log.Fatalf("Failed to reload order: %v", err)
	}

	return &loaded
}

func publishOrder(ctx context.Context, cfg *config.Config, order *models.Order) {
	payload, err := sonic.Marshal(order)
	if err != nil {
		log.Fatalf("Failed to marshal order: %v", err)
	}

	client, err := kgo.NewClient(kgo.SeedBrokers(cfg.Kafka.Brokers...))
	if err != nil {
		log.Fatalf("Failed to create producer: %v", err)
	}
	defer client.Close()

	rec := &kgo.Record{
		Topic: cfg.Topics.Commands.Orders,
		Key:   []byte(order.ID.String()),
		Value: payload,
	}

	if err := client.ProduceSync(ctx, rec).FirstErr(); err != nil {
		log.Fatalf("Failed to publish order: %v", err)
	}
}

func extractMetadata(rec *kgo.Record) *kafka.RecordMetadata {
	for _, h := range rec.Headers {
		if h.Key == "metadata" {
			var m kafka.RecordMetadata
			if err := sonic.Unmarshal(h.Value, &m); err != nil {
				return nil
			}
			return &m
		}
	}
	return nil
}

func sendReply(ctx context.Context, client *kgo.Client, topic string, meta *kafka.RecordMetadata, eventType models.EventType, reply any) {
	payload, err := sonic.Marshal(reply)
	if err != nil {
		log.Fatalf("Failed to marshal reply: %v", err)
	}

	replyMeta := kafka.RecordMetadata{
		EventType: eventType,
		EventID:   uuid.New(),
		SagaID:    meta.SagaID,
		OrderID:   meta.OrderID,
		Timestamp: time.Now().Unix(),
	}

	metaBytes, err := replyMeta.MarshalBinary()
	if err != nil {
		log.Fatalf("Failed to marshal metadata: %v", err)
	}

	rec := &kgo.Record{
		Topic: topic,
		Key:   []byte(meta.OrderID.String()),
		Value: payload,
		Headers: []kgo.RecordHeader{
			{Key: "metadata", Value: metaBytes},
		},
	}

	if err := client.ProduceSync(ctx, rec).FirstErr(); err != nil {
		log.Fatalf("Failed to publish reply: %v", err)
	}
}

func printResult(db *gorm.DB, orderID uuid.UUID) {
	fmt.Println("========================================")
	fmt.Println("  Result")
	fmt.Println("========================================")

	var order models.Order
	if err := db.First(&order, "id = ?", orderID).Error; err != nil {
		fmt.Printf("  Order: ERROR (%v)\n", err)
	} else {
		fmt.Printf("  Order:  %s  status=%s\n", order.PublicID, order.Status)
	}

	var saga models.SagaState
	if err := db.Where("order_id = ?", orderID).First(&saga).Error; err != nil {
		fmt.Printf("  Saga:   not found (%v)\n", err)
	} else {
		fmt.Printf("  Saga:   %s  status=%s  step=%s\n", saga.SagaID, saga.Status, saga.CurrentStep)
	}

	var reservations []models.InventoryReservation
	db.Where("order_id = ?", orderID).Find(&reservations)
	for _, r := range reservations {
		fmt.Printf("  Reservation: product=%s qty=%d status=%s\n", r.ProductID, r.Quantity, r.Status)
	}

	switch order.Status {
	case models.OrderCompleted:
		fmt.Println("\n  PASS")
	case models.OrderFailed:
		fmt.Println("\n  SAGA FAILED (check logs)")
	default:
		fmt.Printf("\n  TIMEOUT (order status: %s)\n", order.Status)
		os.Exit(1)
	}

	fmt.Println("========================================")
}
