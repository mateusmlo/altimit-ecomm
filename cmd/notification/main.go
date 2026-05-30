package main

import (
	"log"

	"github.com/mateusmlo/altimit-ecomm/internal/config"
	"github.com/mateusmlo/altimit-ecomm/internal/kafka"
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

	consumer, err := kafka.NewConsumer(cfg, cfg.ConsumerGroups.NotificationService, []string{cfg.Topics.Commands.Notification}, cfg.Topics.DLQ.Orders, producer.Client)
	if err != nil {
		log.Fatalf("Failed to create consumer: %v", err)
	}
	defer consumer.Client.Close()
}
