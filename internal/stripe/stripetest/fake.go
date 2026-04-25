// Package stripetest provides a configurable fake implementation of the
// stripe.StripeService interface for use in integration tests. It lets tests
// drive PaymentService behavior (success, declined, SDK error) without hitting
// the real Stripe API.
package stripetest

import (
	"context"
	"sync"

	"github.com/google/uuid"
	ss "github.com/mateusmlo/altimit-ecomm/internal/stripe"
	"github.com/stripe/stripe-go/v85"
)

var _ ss.StripeService = (*FakeService)(nil)

type FakeService struct {
	mu        sync.Mutex
	ProcessFn func(ctx context.Context, amount float64, currency string) (*stripe.PaymentIntent, error)
	RefundFn  func(ctx context.Context, paymentIntentID string, amount float64) (*stripe.Refund, error)
}

func New() *FakeService {
	f := &FakeService{}
	f.SetProcessSuccess("")
	f.SetRefundSuccess()
	return f
}

// SetProcessSuccess configures ProcessPayment to return a PaymentIntent with
// status "succeeded". If paymentIntentID is empty, a fresh UUID is used on each
// call so repeated invocations produce distinct IDs.
func (f *FakeService) SetProcessSuccess(paymentIntentID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ProcessFn = func(_ context.Context, _ float64, _ string) (*stripe.PaymentIntent, error) {
		id := paymentIntentID
		if id == "" {
			id = "pi_" + uuid.NewString()
		}
		return &stripe.PaymentIntent{ID: id, Status: stripe.PaymentIntentStatusSucceeded}, nil
	}
}

func (f *FakeService) SetProcessDeclined() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ProcessFn = func(_ context.Context, _ float64, _ string) (*stripe.PaymentIntent, error) {
		return &stripe.PaymentIntent{ID: "pi_" + uuid.NewString(), Status: stripe.PaymentIntentStatusRequiresPaymentMethod}, nil
	}
}

func (f *FakeService) SetProcessError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ProcessFn = func(_ context.Context, _ float64, _ string) (*stripe.PaymentIntent, error) {
		return nil, err
	}
}

func (f *FakeService) SetRefundSuccess() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RefundFn = func(_ context.Context, _ string, _ float64) (*stripe.Refund, error) {
		return &stripe.Refund{Status: stripe.RefundStatusSucceeded}, nil
	}
}

func (f *FakeService) SetRefundFailedStatus() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RefundFn = func(_ context.Context, _ string, _ float64) (*stripe.Refund, error) {
		return &stripe.Refund{Status: stripe.RefundStatusFailed}, nil
	}
}

func (f *FakeService) SetRefundError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RefundFn = func(_ context.Context, _ string, _ float64) (*stripe.Refund, error) {
		return nil, err
	}
}

func (f *FakeService) ProcessPayment(ctx context.Context, amount float64, currency string) (*stripe.PaymentIntent, error) {
	f.mu.Lock()
	fn := f.ProcessFn
	f.mu.Unlock()
	return fn(ctx, amount, currency)
}

func (f *FakeService) RefundPayment(ctx context.Context, paymentIntentID string, amount float64) (*stripe.Refund, error) {
	f.mu.Lock()
	fn := f.RefundFn
	f.mu.Unlock()
	return fn(ctx, paymentIntentID, amount)
}
