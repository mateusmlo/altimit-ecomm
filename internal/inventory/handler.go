package inventory

import (
	"context"
	"fmt"
	"time"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/mateusmlo/altimit-ecomm/internal/config"
	"github.com/mateusmlo/altimit-ecomm/internal/kafka"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"github.com/twmb/franz-go/pkg/kgo"
)

type Handler struct {
	service  *InventoryService
	producer *kafka.Producer
	config   *config.Config
}

func NewHandler(service *InventoryService, producer *kafka.Producer, config *config.Config) *Handler {
	return &Handler{
		service:  service,
		producer: producer,
		config:   config,
	}
}

func (h *Handler) HandleCommand(ctx context.Context, rec *kgo.Record) error {
	metadata, err := h.extractMedatada(rec.Headers)
	if err != nil {
		return fmt.Errorf("failed to extract metadata: %w", err)
	}

	var reply *models.InventoryReply

	switch metadata.EventType {
	case models.EventReserveInventory:
		reply, err = h.handleReserveInventory(ctx, metadata.OrderID, rec.Value)

	case models.EventReleaseInventory:
		reply, err = h.handleReleaseInventory(ctx, metadata.OrderID, rec.Value)

	default:
		return fmt.Errorf("unknown event type: %s", metadata.EventType)
	}

	if err != nil {
		return err
	}

	return h.publishReply(ctx, metadata, reply)
}

func (h *Handler) extractMedatada(headers []kgo.RecordHeader) (*kafka.RecordMetadata, error) {
	var metadataBytes []byte
	var metadata kafka.RecordMetadata

	for _, h := range headers {
		if h.Key == "metadata" {
			metadataBytes = append(metadataBytes, h.Value...)
			break
		}
	}

	if metadataBytes == nil {
		return nil, fmt.Errorf("metadata header not found")
	}

	if err := sonic.Unmarshal(metadataBytes, &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return &metadata, nil
}

func (h *Handler) handleReserveInventory(ctx context.Context, orderID uuid.UUID, payload []byte) (*models.InventoryReply, error) {
	var cmd models.ReserveInventoryCommand

	if err := sonic.Unmarshal(payload, &cmd); err != nil {
		return nil, fmt.Errorf("failed to unmarshal command: %w", err)
	}

	return h.service.ReserveInventory(ctx, orderID, cmd)
}

func (h *Handler) handleReleaseInventory(ctx context.Context, orderID uuid.UUID, payload []byte) (*models.InventoryReply, error) {
	var cmd models.ReleaseInventoryCommand

	if err := sonic.Unmarshal(payload, &cmd); err != nil {
		return nil, fmt.Errorf("failed to unmarshal command: %w", err)
	}

	return h.service.ReleaseInventory(ctx, orderID, cmd)
}

func (h *Handler) publishReply(ctx context.Context, metadata *kafka.RecordMetadata, reply *models.InventoryReply) error {
	var replyEvent models.EventType

	if reply.Success {
		switch metadata.EventType {
		case models.EventReserveInventory:
			replyEvent = models.EventInventoryReserved
		case models.EventReleaseInventory:
			replyEvent = models.EventInventoryReleased
		}
	} else {
		switch metadata.EventType {
		case models.EventReserveInventory:
			replyEvent = models.EventReserveInventoryFailed
		case models.EventReleaseInventory:
			replyEvent = models.EventReleaseInventoryFailed
		}
	}

	replyPayload, err := sonic.Marshal(reply)
	if err != nil {
		return err
	}

	ev := models.Event{
		Event:     replyEvent,
		EventID:   uuid.New(),
		SagaID:    metadata.SagaID,
		OrderID:   metadata.OrderID,
		Payload:   replyPayload,
		Timestamp: time.Now().Unix(),
	}

	return h.producer.PublishEvent(ctx, h.config.Topics.Replies.Inventory, []byte(ev.OrderID.String()), ev)
}
