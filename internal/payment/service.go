package payment

import (
	"context"
	"fmt"

	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"github.com/mateusmlo/altimit-ecomm/internal/repository"
	ss "github.com/mateusmlo/altimit-ecomm/internal/stripe"
	"github.com/stripe/stripe-go/v85"
)

type PaymentService struct {
	stripeService     ss.StripeService
	paymentRepository repository.PaymentRepository
}

func NewPaymentService(ss ss.StripeService, pr repository.PaymentRepository) *PaymentService {
	return &PaymentService{stripeService: ss, paymentRepository: pr}
}

func (ps *PaymentService) ProcessPayment(ctx context.Context, cmd models.ProcessPaymentCommand) (*models.PaymentReply, error) {
	pi, err := ps.stripeService.ProcessPayment(ctx, cmd.Amount, cmd.Currency)
	if err != nil {
		return &models.PaymentReply{Success: false, Message: fmt.Sprintf("payment failed: %v", err)}, nil
	}
	if pi.Status != stripe.PaymentIntentStatusSucceeded {
		return &models.PaymentReply{Success: false, Message: fmt.Sprintf("payment failed: %s", pi.Status)}, nil
	}

	newPayment := &models.Payment{
		OrderID:         cmd.OrderID,
		Amount:          cmd.Amount,
		Currency:        cmd.Currency,
		Status:          models.PaymentCompleted,
		PaymentIntentID: pi.ID,
	}

	err = ps.paymentRepository.Create(ctx, newPayment)
	if err != nil {
		return nil, err
	}

	return &models.PaymentReply{
		Success:         true,
		PaymentIntentID: pi.ID,
		Message:         "payment success",
	}, nil
}

func (ps *PaymentService) RefundPayment(ctx context.Context, cmd models.RefundPaymentCommand) (*models.PaymentReply, error) {
	payment, err := ps.paymentRepository.GetByOrderID(ctx, cmd.OrderID)
	if err != nil {
		return nil, err
	}

	ref, err := ps.stripeService.RefundPayment(ctx, payment.PaymentIntentID, payment.Amount)
	// stripe errors are not infrastructure errors, so the application should neither crash nor bubble them up
	if err != nil {
		return &models.PaymentReply{Success: false, Message: fmt.Sprintf("refund failed: %v", err)}, nil
	}
	if ref.Status != stripe.RefundStatusSucceeded {
		return &models.PaymentReply{Success: false, Message: fmt.Sprintf("refund failed: %s", ref.Status)}, nil
	}

	err = ps.paymentRepository.UpdateStatus(ctx, payment.ID, models.PaymentRefunded)
	if err != nil {
		return nil, err
	}

	return &models.PaymentReply{
		Success:         true,
		PaymentIntentID: payment.PaymentIntentID,
		Message:         "refund success",
	}, nil
}
