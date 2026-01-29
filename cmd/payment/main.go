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

	producer, err := kafka.NewConsumer(cfg, cfg.ConsumerGroups.PaymentService, []string{cfg.Topics.Commands.Payment})
	if err != nil {
		log.Fatalf("Failed to create consumer: %v", err)
	}

	defer producer.Client.Close()
}
