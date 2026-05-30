package payment

import (
	"context"
	"testing"
	"time"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/mateusmlo/altimit-ecomm/internal/config"
	"github.com/mateusmlo/altimit-ecomm/internal/errs"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"github.com/mateusmlo/altimit-ecomm/internal/stripe/stripetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

// mockIdempotencyRepo is a hand-written mock for IdempotencyRepository.
type mockIdempotencyRepo struct {
	existsResult bool
	existsErr    error
	createErr    error
	createdKey   string
	createCalled bool
}

func (m *mockIdempotencyRepo) Create(_ context.Context, key *models.IdempotencyKey) error {
	m.createCalled = true
	m.createdKey = key.Key
	return m.createErr
}

func (m *mockIdempotencyRepo) GetByKey(_ context.Context, _ string) (*models.IdempotencyKey, error) {
	return nil, nil
}

func (m *mockIdempotencyRepo) Exists(_ context.Context, _ string) (bool, error) {
	return m.existsResult, m.existsErr
}

func (m *mockIdempotencyRepo) Delete(_ context.Context, _ string) error { return nil }

func (m *mockIdempotencyRepo) DeleteOlderThan(_ context.Context, _ time.Duration) error { return nil }

// mockOrderRepo is a hand-written mock for OrderRepository.
type mockOrderRepo struct {
	order *models.Order
	err   error
}

func (m *mockOrderRepo) Create(_ context.Context, _ *models.Order) error { return nil }
func (m *mockOrderRepo) GetByID(_ context.Context, _ uuid.UUID) (*models.Order, error) {
	return m.order, m.err
}
func (m *mockOrderRepo) GetByPublicID(_ context.Context, _ string) (*models.Order, error) {
	return m.order, m.err
}
func (m *mockOrderRepo) GetByCustomerID(_ context.Context, _ string, _, _ int) ([]*models.Order, error) {
	return nil, nil
}
func (m *mockOrderRepo) Update(_ context.Context, _ *models.Order) error              { return nil }
func (m *mockOrderRepo) UpdateStatus(_ context.Context, _ uuid.UUID, _ models.OrderStatus) error {
	return nil
}
func (m *mockOrderRepo) Delete(_ context.Context, _ uuid.UUID) error { return nil }
func (m *mockOrderRepo) List(_ context.Context, _, _ int) ([]*models.Order, error) {
	return nil, nil
}

// mockPaymentRepo is a hand-written mock for PaymentRepository.
type mockPaymentRepo struct {
	createCalled bool
}

func (m *mockPaymentRepo) Create(_ context.Context, _ *models.Payment) error {
	m.createCalled = true
	return nil
}
func (m *mockPaymentRepo) GetByID(_ context.Context, _ uuid.UUID) (*models.Payment, error) {
	return nil, nil
}
func (m *mockPaymentRepo) GetByOrderID(_ context.Context, _ uuid.UUID) (*models.Payment, error) {
	return nil, nil
}
func (m *mockPaymentRepo) Update(_ context.Context, _ *models.Payment) error { return nil }
func (m *mockPaymentRepo) UpdateStatus(_ context.Context, _ uuid.UUID, _ models.PaymentStatus) error {
	return nil
}
func (m *mockPaymentRepo) Delete(_ context.Context, _ uuid.UUID) error { return nil }
func (m *mockPaymentRepo) List(_ context.Context, _, _ int) ([]*models.Payment, error) {
	return nil, nil
}
func (m *mockPaymentRepo) GetByStatus(_ context.Context, _ models.PaymentStatus, _, _ int) ([]*models.Payment, error) {
	return nil, nil
}

// mockPublisher records the last published event for assertions.
type mockPublisher struct {
	publishErr error
	lastTopic  string
	lastEvent  models.Event
}

func (m *mockPublisher) PublishEvent(_ context.Context, topic string, ev models.Event) error {
	m.lastTopic = topic
	m.lastEvent = ev
	return m.publishErr
}

func makeMetadataHeader(t *testing.T, m models.RecordMetadata) kgo.RecordHeader {
	t.Helper()
	b, err := sonic.Marshal(m)
	require.NoError(t, err)
	return kgo.RecordHeader{Key: "metadata", Value: b}
}

func TestHandleCommand_MissingMetadataHeader(t *testing.T) {
	h := &Handler{idempotencyRepo: &mockIdempotencyRepo{}, config: &config.Config{}}

	err := h.HandleCommand(context.Background(), &kgo.Record{
		Headers: []kgo.RecordHeader{},
		Value:   []byte("{}"),
	})

	assert.ErrorIs(t, err, errs.ErrMissingMetadata)
}

func TestHandleCommand_UnknownEventType(t *testing.T) {
	h := &Handler{idempotencyRepo: &mockIdempotencyRepo{}, config: &config.Config{}}

	meta := models.RecordMetadata{
		EventType: models.EventType("UNKNOWN_EVENT"),
		EventID:   uuid.New(),
		SagaID:    uuid.New(),
		OrderID:   uuid.New(),
	}

	err := h.HandleCommand(context.Background(), &kgo.Record{
		Headers: []kgo.RecordHeader{makeMetadataHeader(t, meta)},
		Value:   []byte("{}"),
	})

	assert.ErrorIs(t, err, errs.ErrUnknownEvent)
}

func TestHandleCommand_DuplicateSuppressed(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	paymentRepo := &mockPaymentRepo{}
	idempotency := &mockIdempotencyRepo{existsResult: true}

	h := &Handler{
		service:         NewPaymentService(stripetest.New(), paymentRepo),
		producer:        nil, // must not be reached
		orderRepo:       orderRepo,
		idempotencyRepo: idempotency,
		config:          &config.Config{},
	}

	meta := models.RecordMetadata{
		EventType: models.EventProcessPayment,
		EventID:   uuid.New(),
		SagaID:    uuid.New(),
		OrderID:   uuid.New(),
	}

	err := h.HandleCommand(context.Background(), &kgo.Record{
		Headers: []kgo.RecordHeader{makeMetadataHeader(t, meta)},
		Value:   []byte("{}"),
	})

	require.NoError(t, err)
	assert.False(t, paymentRepo.createCalled, "service must not run for a duplicate command")
	assert.False(t, idempotency.createCalled, "no key should be stored for a duplicate")
}

func TestHandleCommand_StoresKeyAfterSuccess(t *testing.T) {
	orderRepo := &mockOrderRepo{order: &models.Order{
		ID:          uuid.New(),
		TotalAmount: 42.00,
		Currency:    "USD",
	}}
	paymentRepo := &mockPaymentRepo{}
	idempotency := &mockIdempotencyRepo{existsResult: false}

	h := &Handler{
		service:         NewPaymentService(stripetest.New(), paymentRepo),
		producer:        &mockPublisher{},
		orderRepo:       orderRepo,
		idempotencyRepo: idempotency,
		config:          &config.Config{},
	}

	eventID := uuid.New()
	meta := models.RecordMetadata{
		EventType: models.EventProcessPayment,
		EventID:   eventID,
		SagaID:    uuid.New(),
		OrderID:   uuid.New(),
	}

	err := h.HandleCommand(context.Background(), &kgo.Record{
		Headers: []kgo.RecordHeader{makeMetadataHeader(t, meta)},
		Value:   []byte("{}"),
	})

	require.NoError(t, err)
	assert.True(t, paymentRepo.createCalled, "service should run for a new command")
	assert.True(t, idempotency.createCalled, "key should be stored after success")
	assert.Equal(t, eventID.String(), idempotency.createdKey)
}
