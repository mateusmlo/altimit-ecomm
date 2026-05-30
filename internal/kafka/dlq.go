package kafka

import (
	"context"
	"fmt"

	"github.com/twmb/franz-go/pkg/kgo"
)

// PublishToDLQ forwards a failed record to the dead-letter queue, appending the handler error as a header.
func PublishToDLQ(ctx context.Context, client *kgo.Client, topic string, record *kgo.Record, handlerErr error) error {
	headers := make([]kgo.RecordHeader, len(record.Headers)+1)
	copy(headers, record.Headers)
	headers[len(record.Headers)] = kgo.RecordHeader{
		Key:   "dlq-error",
		Value: []byte(handlerErr.Error()),
	}

	dlqRecord := &kgo.Record{
		Topic:   topic,
		Key:     record.Key,
		Value:   record.Value,
		Headers: headers,
	}

	if err := client.ProduceSync(ctx, dlqRecord).FirstErr(); err != nil {
		return fmt.Errorf("failed to publish to DLQ: %w", err)
	}

	return nil
}
