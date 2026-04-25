package orchestrator

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/bytedance/sonic"
	"github.com/mateusmlo/altimit-ecomm/internal/config"
	"github.com/mateusmlo/altimit-ecomm/internal/errs"
	"github.com/mateusmlo/altimit-ecomm/internal/kafka"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"github.com/mateusmlo/altimit-ecomm/internal/repository"
	"github.com/twmb/franz-go/pkg/kgo"
)

type Handler struct {
	orchestrator SagaOrchestrator
	orderRepo    repository.OrderRepository
	sagaRepo     repository.SagaRepository
	config       *config.Config
}

func NewHandler(orchestrator SagaOrchestrator, orderRepo repository.OrderRepository, sagaRepo repository.SagaRepository, config *config.Config) *Handler {
	return &Handler{
		orchestrator: orchestrator,
		orderRepo:    orderRepo,
		sagaRepo:     sagaRepo,
		config:       config,
	}
}

func (h *Handler) HandleRecord(ctx context.Context, rec *kgo.Record) error {
	switch rec.Topic {
	case h.config.Topics.Commands.Orders:
		return h.handleNewOrder(ctx, rec)

	case h.config.Topics.Replies.Inventory,
		h.config.Topics.Replies.Payment,
		h.config.Topics.Replies.Notification:
		return h.handleReply(ctx, rec)

	default:
		return fmt.Errorf("unexpected topic: %s", rec.Topic)
	}
}

func (h *Handler) handleNewOrder(ctx context.Context, rec *kgo.Record) error {
	var order models.Order

	if err := sonic.Unmarshal(rec.Value, &order); err != nil {
		return err
	}

	existingSaga, err := h.sagaRepo.GetByOrderID(ctx, order.ID)
	if err == nil && existingSaga != nil {
		log.Printf("Saga already exists for order %s, skipping duplicate", order.PublicID)
		return nil
	}

	return h.orchestrator.StartSaga(ctx, &order)
}

func (h *Handler) handleReply(ctx context.Context, rec *kgo.Record) error {
	metadata, err := kafka.ExtractMetadata(rec.Headers)
	if err != nil {
		return err
	}

	saga, err := h.orchestrator.GetSagaState(ctx, metadata.SagaID)
	if err != nil {
		return err
	}

	if saga.NextRetryAt != nil && time.Now().Before(*saga.NextRetryAt) {
		log.Printf("SAGA %s in backoff period, skipping until %s",
			saga.SagaID, saga.NextRetryAt.Format(time.RFC3339))

		return nil
	}

	reply, err := h.unmarshalReplyByEventType(metadata.EventType, rec.Value)
	if err != nil {
		return err
	}

	success := isSuccessReply(reply)

	switch saga.Status {
	case models.SagaCompensating:
		if success {
			return h.orchestrator.ProcessCompensationSuccess(ctx, metadata.SagaID)
		}

		return h.orchestrator.ProcessCompensationFailure(ctx, metadata.SagaID)

	case models.SagaInProgress, models.SagaStarted:
		if success {
			return h.orchestrator.ProcessStepSuccess(ctx, metadata.SagaID)
		}

		return h.orchestrator.ProcessStepFailure(ctx, metadata.SagaID)

	case models.SagaCompleted:
		fmt.Printf("SAGA %s completed successfully\n", saga.SagaID)
		return nil

	default:
		return fmt.Errorf("received reply for SAGA in unexpected status %s", saga.Status)
	}
}

func (h *Handler) unmarshalReplyByEventType(eventType models.EventType, payload []byte) (any, error) {
	switch eventType {
	case models.EventInventoryReserved,
		models.EventInventoryReleased,
		models.EventReserveInventoryFailed,
		models.EventReleaseInventoryFailed:
		var reply models.InventoryReply
		if err := sonic.Unmarshal(payload, &reply); err != nil {
			return nil, err
		}
		return &reply, nil

	case models.EventPaymentSucceeded,
		models.EventPaymentFailed,
		models.EventRefundSucceeded,
		models.EventRefundFailed:
		var reply models.PaymentReply
		if err := sonic.Unmarshal(payload, &reply); err != nil {
			return nil, err
		}
		return &reply, nil

	case models.EventNotificationSent,
		models.EventNotificationFailed:
		var reply models.NotificationReply
		if err := sonic.Unmarshal(payload, &reply); err != nil {
			return nil, err
		}
		return &reply, nil

	default:
		return nil, fmt.Errorf("%w: %s", errs.ErrUnknownEvent, eventType)
	}
}

func isSuccessReply(reply any) bool {
	switch r := reply.(type) {
	case *models.InventoryReply:
		return r.Success
	case *models.PaymentReply:
		return r.Success
	case *models.NotificationReply:
		return r.Success
	default:
		return false
	}
}
