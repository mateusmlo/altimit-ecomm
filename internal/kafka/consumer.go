package kafka

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/mateusmlo/altimit-ecomm/internal/config"
	"github.com/twmb/franz-go/pkg/kgo"
)

// I  know two clients is not ideal but the separation of concerns is just while I get used to all this stuff

type Consumer struct {
	Client *kgo.Client
	cfg    *config.Config
}

type RecordHandler func(ctx context.Context, record *kgo.Record) error

func NewConsumer(cfg *config.Config, groupID string, topics []string) (*Consumer, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Kafka.Brokers...),
		kgo.WithLogger(kgo.BasicLogger(log.Writer(), kgo.LogLevelDebug, nil)),
		kgo.ConsumerGroup(groupID),
		kgo.ConsumeTopics(topics...),
		kgo.DisableAutoCommit(),
	)

	if err != nil {
		return nil, err
	}

	return &Consumer{
		Client: client,
		cfg:    cfg,
	}, nil
}

func (c *Consumer) Consume(ctx context.Context, handler RecordHandler) error {
	for {
		select {
		case <-ctx.Done():
			log.Println("Shutdown consumer...")

			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

			defer cancel()

			if err := c.Client.CommitUncommittedOffsets(shutdownCtx); err != nil {
				log.Printf("Failed to commit offsets during shutdown: %v", err)
			}

			return ctx.Err()
		default:
			var processErr error

			fetches := c.Client.PollFetches(ctx)

			if fetches.IsClientClosed() {
				return errors.New("client closed")
			}

			if err := fetches.Err(); err != nil {
				log.Printf("fetch records error: %v", err)
				return err
			}

			fetches.EachRecord(func(record *kgo.Record) {
				if processErr != nil {
					return
				}

				if err := handler(ctx, record); err != nil {
					log.Printf("error handling record: %v", err)
					processErr = err
					return
				}
			})

			if processErr != nil {
				//TODO: add to DLQ
				return processErr
			}

			c.Client.CommitUncommittedOffsets(ctx)
		}
	}
}
