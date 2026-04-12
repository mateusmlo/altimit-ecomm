package inventory

import (
	"context"
	"fmt"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/mateusmlo/altimit-ecomm/internal/config"
	"github.com/mateusmlo/altimit-ecomm/internal/errs"
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
	metadata, err := kafka.ExtractMetadata(rec.Headers)
	if err != nil {
		return err
	}

	var reply *models.InventoryReply

	switch metadata.EventType {
	case models.EventReserveInventory:
		reply, err = h.handleReserveInventory(ctx, metadata.OrderID, rec.Value)

	case models.EventReleaseInventory:
		reply, err = h.handleReleaseInventory(ctx, metadata.OrderID, rec.Value)

	default:
		return fmt.Errorf("%w: %s", errs.ErrUnknownEvent, metadata.EventType)
	}

	if err != nil {
		return err
	}

	return h.publishReply(ctx, metadata, reply)
}

func (h *Handler) handleReserveInventory(ctx context.Context, orderID uuid.UUID, payload []byte) (*models.InventoryReply, error) {
	var cmd models.ReserveInventoryCommand
	if err := sonic.Unmarshal(payload, &cmd); err != nil {
		return nil, fmt.Errorf("%w: %w", errs.ErrMalformedPayload, err)
	}

	return h.service.ReserveInventory(ctx, orderID, cmd)
}

func (h *Handler) handleReleaseInventory(ctx context.Context, orderID uuid.UUID, payload []byte) (*models.InventoryReply, error) {
	var cmd models.ReleaseInventoryCommand
	if err := sonic.Unmarshal(payload, &cmd); err != nil {
		return nil, fmt.Errorf("%w: %w", errs.ErrMalformedPayload, err)
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

	return kafka.PublishReply(ctx, h.producer, h.config.Topics.Replies.Inventory, metadata, replyEvent, reply)
}
