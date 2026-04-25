package payment

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/mateusmlo/altimit-ecomm/internal/config"
	"github.com/mateusmlo/altimit-ecomm/internal/errs"
	"github.com/mateusmlo/altimit-ecomm/internal/kafka"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"github.com/mateusmlo/altimit-ecomm/internal/repository"
	"github.com/twmb/franz-go/pkg/kgo"
)

type Handler struct {
	service   *PaymentService
	producer  *kafka.Producer
	orderRepo repository.OrderRepository
	config    *config.Config
}

func NewHandler(service *PaymentService, producer *kafka.Producer, orderRepo repository.OrderRepository, config *config.Config) *Handler {
	return &Handler{
		service:   service,
		producer:  producer,
		orderRepo: orderRepo,
		config:    config,
	}
}

func (h *Handler) HandleCommand(ctx context.Context, rec *kgo.Record) error {
	metadata, err := kafka.ExtractMetadata(rec.Headers)
	if err != nil {
		return err
	}

	var reply *models.PaymentReply

	switch metadata.EventType {
	case models.EventProcessPayment:
		reply, err = h.handleProcessPayment(ctx, metadata.OrderID)

	case models.EventRefundPayment:
		reply, err = h.handleRefundPayment(ctx, metadata.OrderID)

	default:
		return fmt.Errorf("%w: %s", errs.ErrUnknownEvent, metadata.EventType)
	}

	if err != nil {
		return err
	}

	return h.publishReply(ctx, metadata, reply)
}

func (h *Handler) handleProcessPayment(ctx context.Context, orderID uuid.UUID) (*models.PaymentReply, error) {
	order, err := h.orderRepo.GetByID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errs.ErrOrderNotFound, err)
	}

	return h.service.ProcessPayment(ctx, models.ProcessPaymentCommand{
		OrderID:  order.ID,
		Amount:   order.TotalAmount,
		Currency: order.Currency,
	})
}

func (h *Handler) handleRefundPayment(ctx context.Context, orderID uuid.UUID) (*models.PaymentReply, error) {
	return h.service.RefundPayment(ctx, models.RefundPaymentCommand{
		OrderID: orderID,
	})
}

func (h *Handler) publishReply(ctx context.Context, metadata *models.RecordMetadata, reply *models.PaymentReply) error {
	var replyEvent models.EventType

	if reply.Success {
		switch metadata.EventType {
		case models.EventProcessPayment:
			replyEvent = models.EventPaymentSucceeded
		case models.EventRefundPayment:
			replyEvent = models.EventRefundSucceeded
		}
	} else {
		switch metadata.EventType {
		case models.EventProcessPayment:
			replyEvent = models.EventPaymentFailed
		case models.EventRefundPayment:
			replyEvent = models.EventRefundFailed
		}
	}

	event, err := models.NewEvent(replyEvent, metadata, reply)
	if err != nil {
		return err
	}

	return kafka.PublishReply(ctx, h.producer, h.config.Topics.Replies.Payment, event)
}
