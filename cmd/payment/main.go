package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/mateusmlo/altimit-ecomm/internal/config"
	db "github.com/mateusmlo/altimit-ecomm/internal/database"
	"github.com/mateusmlo/altimit-ecomm/internal/kafka"
	"github.com/mateusmlo/altimit-ecomm/internal/payment"
	"github.com/mateusmlo/altimit-ecomm/internal/repository"
	"github.com/mateusmlo/altimit-ecomm/internal/stripe"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	producer, err := kafka.NewProducer(cfg)
	if err != nil {
		log.Fatalf("Failed to create producer: %v", err)
	}
	defer producer.Client.Close()

	db, err := db.NewDatabase(cfg.Postgres)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	stripeClient := stripe.NewStripeClient(cfg.Stripe)
	orderRepo := repository.NewOrderRepository(db)
	paymentRepo := repository.NewPaymentRepository(db)
	stripeService := stripe.NewStripeService(stripeClient)

	service := payment.NewPaymentService(stripeService, paymentRepo)
	handler := payment.NewHandler(service, producer, orderRepo, cfg)

	consumer, err := kafka.NewConsumer(cfg, cfg.ConsumerGroups.PaymentService, []string{cfg.Topics.Commands.Payment})
	if err != nil {
		log.Fatalf("Failed to create consumer: %v", err)
	}
	defer consumer.Client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Printf("Received shutdown signal")
		cancel()
	}()

	if err := consumer.Consume(ctx, handler.HandleCommand); err != nil && err != context.Canceled {
		log.Fatalf("Consumer error: %v", err)
	}

	log.Println("Payment service stopped")
}
