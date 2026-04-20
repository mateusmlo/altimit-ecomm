package kafka

import (
	"context"
	"fmt"
	"log"

	"github.com/bytedance/sonic"
	"github.com/mateusmlo/altimit-ecomm/internal/config"
	"github.com/mateusmlo/altimit-ecomm/internal/errs"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"github.com/twmb/franz-go/pkg/kgo"
)

type Producer struct {
	Client *kgo.Client
	cfg    *config.Config
}

func NewProducer(cfg *config.Config) (*Producer, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Kafka.Brokers...),
		kgo.WithLogger(kgo.BasicLogger(log.Writer(), kgo.LogLevelError, nil)),
		kgo.ClientID("altimit"),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.RecordRetries(cfg.Kafka.MaxRecordRetries),
		kgo.RequestRetries(cfg.Kafka.MaxRequestRetries),
	)

	if err != nil {
		return nil, err
	}

	return &Producer{Client: client, cfg: cfg}, nil
}

// ExtractMetadata parses the "metadata" header from a Kafka record.
func ExtractMetadata(headers []kgo.RecordHeader) (*models.RecordMetadata, error) {
	var metadataBytes []byte
	for _, h := range headers {
		if h.Key == "metadata" {
			metadataBytes = h.Value
			break
		}
	}
	if metadataBytes == nil {
		return nil, errs.ErrMissingMetadata
	}
	var metadata models.RecordMetadata
	if err := sonic.Unmarshal(metadataBytes, &metadata); err != nil {
		return nil, fmt.Errorf("%w: %w", errs.ErrMalformedPayload, err)
	}
	return &metadata, nil
}

// PublishReply marshals reply and publishes it as a saga event to the given topic.
func PublishReply(ctx context.Context, producer EventPublisher, topic string, event models.Event) error {
	return producer.PublishEvent(ctx, topic, event)
}

func (p *Producer) PublishEvent(ctx context.Context, topic string, ev models.Event) error {
	msgPayload, err := ev.Payload.MarshalJSON()
	if err != nil {
		return err
	}

	rm := models.NewRecordMetadata(ev)

	rmBytes, err := sonic.Marshal(rm)
	if err != nil {
		return err
	}

	msg := &kgo.Record{
		Topic: topic,
		Key:   []byte(ev.OrderID.String()),
		Value: msgPayload,
		Headers: []kgo.RecordHeader{
			{
				Key:   "metadata",
				Value: rmBytes,
			},
		},
	}

	if err := p.Client.ProduceSync(ctx, msg).FirstErr(); err != nil {
		log.Printf("Failed to publish event %s after retries: %v", ev.EventID, err)
		//TODO: emit metrics
		return err
	}

	log.Printf("Published event %v to topic %s", ev.EventID, topic)

	return nil
}
