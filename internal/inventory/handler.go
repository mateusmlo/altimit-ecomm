package inventory

import (
	"context"
	"fmt"
	"log"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/mateusmlo/altimit-ecomm/internal/config"
	"github.com/mateusmlo/altimit-ecomm/internal/errs"
	"github.com/mateusmlo/altimit-ecomm/internal/kafka"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"github.com/mateusmlo/altimit-ecomm/internal/repository"
	"github.com/twmb/franz-go/pkg/kgo"
)

type Handler struct {
	service         *InventoryService
	producer        kafka.EventPublisher
	idempotencyRepo repository.IdempotencyRepository
	config          *config.Config
}

func NewHandler(service *InventoryService, producer kafka.EventPublisher, idempotencyRepo repository.IdempotencyRepository, config *config.Config) *Handler {
	return &Handler{
		service:         service,
		producer:        producer,
		idempotencyRepo: idempotencyRepo,
		config:          config,
	}
}

func (h *Handler) HandleCommand(ctx context.Context, rec *kgo.Record) error {
	metadata, err := kafka.ExtractMetadata(rec.Headers)
	if err != nil {
		return err
	}

	key := metadata.EventID.String()

	exists, err := h.idempotencyRepo.Exists(ctx, key)
	if err != nil {
		return err
	}
	if exists {
		log.Printf("Duplicate command %s already processed, skipping", key)
		return nil
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

	if err := h.publishReply(ctx, metadata, reply); err != nil {
		return err
	}

	return h.storeIdempotencyKey(ctx, key, reply)
}

// storeIdempotencyKey records the processed command keyed by EventID so a
// redelivered command is suppressed. Called after a successful publishReply.
func (h *Handler) storeIdempotencyKey(ctx context.Context, key string, reply *models.InventoryReply) error {
	response, err := sonic.Marshal(reply)
	if err != nil {
		return err
	}

	return h.idempotencyRepo.Create(ctx, &models.IdempotencyKey{
		Key:      key,
		Response: response,
	})
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

func (h *Handler) publishReply(ctx context.Context, metadata *models.RecordMetadata, reply *models.InventoryReply) error {
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

	event, err := models.NewEvent(replyEvent, metadata, reply)
	if err != nil {
		return err
	}

	return kafka.PublishReply(ctx, h.producer, h.config.Topics.Replies.Inventory, event)
}
