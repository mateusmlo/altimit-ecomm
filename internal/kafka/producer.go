package kafka

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/mateusmlo/altimit-ecomm/internal/config"
	"github.com/mateusmlo/altimit-ecomm/internal/errs"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"github.com/twmb/franz-go/pkg/kgo"
)

type Producer struct {
	Client *kgo.Client
	cfg    *config.Config
}

type RecordMetadata struct {
	EventType models.EventType `json:"event_type"`
	EventID   uuid.UUID        `json:"event_id"`
	SagaID    uuid.UUID        `json:"saga_id"`
	OrderID   uuid.UUID        `json:"order_id"`
	Timestamp int64            `json:"timestamp"`
}

func (rm *RecordMetadata) MarshalBinary() ([]byte, error) {
	return sonic.Marshal(rm)
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
func ExtractMetadata(headers []kgo.RecordHeader) (*RecordMetadata, error) {
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
	var metadata RecordMetadata
	if err := sonic.Unmarshal(metadataBytes, &metadata); err != nil {
		return nil, fmt.Errorf("%w: %w", errs.ErrMalformedPayload, err)
	}
	return &metadata, nil
}

// PublishReply marshals reply and publishes it as a saga event to the given topic.
func PublishReply(ctx context.Context, producer EventPublisher, topic string, metadata *RecordMetadata, replyEvent models.EventType, reply any) error {
	replyPayload, err := sonic.Marshal(reply)
	if err != nil {
		return err
	}
	ev := models.Event{
		Event:     replyEvent,
		EventID:   uuid.New(),
		SagaID:    metadata.SagaID,
		OrderID:   metadata.OrderID,
		Timestamp: time.Now().Unix(),
		Payload:   replyPayload,
	}
	return producer.PublishEvent(ctx, topic, []byte(ev.OrderID.String()), ev)
}

func (p *Producer) PublishEvent(ctx context.Context, topic string, key []byte, ev models.Event) error {
	msgPayload, err := ev.Payload.MarshalJSON()
	if err != nil {
		return err
	}

	rm := RecordMetadata{
		EventType: ev.Event,
		EventID:   ev.EventID,
		SagaID:    ev.SagaID,
		OrderID:   ev.OrderID,
		Timestamp: ev.Timestamp,
	}

	rmBytes, err := rm.MarshalBinary()
	if err != nil {
		return err
	}

	msg := &kgo.Record{
		Topic: topic,
		Key:   key,
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
