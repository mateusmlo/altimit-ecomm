package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/mateusmlo/altimit-ecomm/internal/config"
	db "github.com/mateusmlo/altimit-ecomm/internal/database"
	"github.com/mateusmlo/altimit-ecomm/internal/inventory"
	"github.com/mateusmlo/altimit-ecomm/internal/kafka"
	"github.com/mateusmlo/altimit-ecomm/internal/repository"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	producer, err := kafka.NewProducer(cfg)
	if err != nil {
		log.Fatalf("Failed to create consumer: %v", err)
	}
	defer producer.Client.Close()

	db, err := db.NewDatabase(cfg.Postgres)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	repo := repository.NewInventoryReservationRepository(db)
	service := inventory.NewInventoryService(repo)

	handler := inventory.NewHandler(service, producer, cfg)

	consumer, err := kafka.NewConsumer(cfg, cfg.ConsumerGroups.InventoryService, []string{cfg.Topics.Commands.Inventory}, cfg.Topics.DLQ.Orders, producer.Client)
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
		log.Println("Received shutdown signal")
		cancel()
	}()

	log.Printf("Inventory service started, consuming from %s", cfg.Topics.Commands.Inventory)

	if err := consumer.Consume(ctx, handler.HandleCommand); err != nil && err != context.Canceled {
		log.Fatalf("Consumer error: %v", err)
	}

	log.Printf("Inventory service stopped")
}
