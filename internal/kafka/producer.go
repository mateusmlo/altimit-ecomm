package kafka

import (
	"context"
	"log"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/mateusmlo/altimit-ecomm/internal/config"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"github.com/twmb/franz-go/pkg/kgo"
)

type Producer struct {
	Client *kgo.Client
	cfg    *config.Config
}

type RecordMetadata struct {
	eventType models.EventType
	eventID   uuid.UUID
	sagaID    uuid.UUID
	orderID   uuid.UUID
	timestamp int64
}

func (rm *RecordMetadata) MarshalBinary() ([]byte, error) {
	return sonic.Marshal(rm)
}

func NewProducer(cfg *config.Config) (*Producer, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Kafka.Brokers...),
		kgo.WithLogger(kgo.BasicLogger(log.Writer(), kgo.LogLevelDebug, nil)),
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

func (p *Producer) PublishEvent(ctx context.Context, topic string, key []byte, ev models.Event) error {
	msgPayload, err := ev.Payload.MarshalJSON()
	if err != nil {
		return err
	}

	rm := RecordMetadata{
		eventType: ev.Event,
		eventID:   ev.EventID,
		sagaID:    ev.SagaID,
		orderID:   ev.OrderID,
		timestamp: ev.Timestamp,
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
