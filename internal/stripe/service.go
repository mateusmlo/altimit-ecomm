package stripe

import (
	"context"

	"github.com/stripe/stripe-go/v85"
)

type StripeService interface {
	ProcessPayment(ctx context.Context, amount float64, currency string) (*stripe.PaymentIntent, error)
	RefundPayment(ctx context.Context, stripePaymentIntentID string, amount float64) (*stripe.Refund, error)
}

type stripeService struct {
	client *stripe.Client
}

func NewStripeService(client *stripe.Client) StripeService {
	return &stripeService{client: client}
}

func (s *stripeService) ProcessPayment(ctx context.Context, amount float64, currency string) (*stripe.PaymentIntent, error) {
	smallAmount := int64(amount * 100)
	params := &stripe.PaymentIntentCreateParams{
		Amount:        stripe.Int64(smallAmount),
		Currency:      stripe.String(currency),
		PaymentMethod: stripe.String("pm_card_visa"),
		Confirm:       stripe.Bool(true),
		ReturnURL:     stripe.String("http://localhost:8080/payment/complete"),
	}

	pi, err := s.client.V1PaymentIntents.Create(ctx, params)
	if err != nil {
		return nil, err
	}

	return pi, nil
}

func (s *stripeService) RefundPayment(ctx context.Context, stripePaymentIntentID string, amount float64) (*stripe.Refund, error) {
	smallAmount := int64(amount * 100)

	params := &stripe.RefundCreateParams{
		PaymentIntent: stripe.String(stripePaymentIntentID),
		Amount:        stripe.Int64(smallAmount),
	}

	refund, err := s.client.V1Refunds.Create(ctx, params)
	if err != nil {
		return nil, err
	}

	return refund, nil
}
