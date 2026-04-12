package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/mateusmlo/altimit-ecomm/internal/config"
	db "github.com/mateusmlo/altimit-ecomm/internal/database"
	"github.com/mateusmlo/altimit-ecomm/internal/kafka"
	"github.com/mateusmlo/altimit-ecomm/internal/orchestrator"
	"github.com/mateusmlo/altimit-ecomm/internal/repository"
)

func main() {
	var wg sync.WaitGroup
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	listenTopics := []string{cfg.Topics.Commands.Orders, cfg.Topics.Replies.Inventory, cfg.Topics.Replies.Payment, cfg.Topics.Replies.Notification}

	producer, err := kafka.NewProducer(cfg)
	if err != nil {
		log.Fatalf("Failed to create consumer: %v", err)
	}
	defer producer.Client.Close()

	db, err := db.NewDatabase(cfg.Postgres)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	sagaRepo := repository.NewSagaRepository(db)
	orderRepo := repository.NewOrderRepository(db)
	paymentRepo := repository.NewPaymentRepository(db)

	consumer, err := kafka.NewConsumer(cfg, cfg.ConsumerGroups.SagaOrchestrator, listenTopics)
	if err != nil {
		log.Fatalf("Failed to create consumer: %v", err)
	}
	defer consumer.Client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	orch := orchestrator.NewOrchestrator(sagaRepo, orderRepo, paymentRepo, producer, cfg)
	handler := orchestrator.NewHandler(orch, orderRepo, sagaRepo, cfg)

	wg.Add(2)
	go func() {
		defer wg.Done()
		orch.RetryWorker(ctx)
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		defer wg.Done()
		<-sigChan
		log.Println("Received shutdown signal")
		cancel()
	}()

	log.Printf("Orchestrator started, consuming from %v", listenTopics)

	if err := consumer.Consume(ctx, handler.HandleRecord); err != nil && err != context.Canceled {
		log.Fatalf("Consumer handle record error: %v", err)
	}

	wg.Wait()
	log.Println("Orchestrator stopped")
}
