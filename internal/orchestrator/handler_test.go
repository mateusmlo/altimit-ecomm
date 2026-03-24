package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/mateusmlo/altimit-ecomm/internal/config"
	"github.com/mateusmlo/altimit-ecomm/internal/errs"
	"github.com/mateusmlo/altimit-ecomm/internal/kafka"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

// ---- Mocks ----

type mockOrchestrator struct {
	startSagaErr          error
	processStepSuccessErr error
	processStepFailureErr error
	processCompSuccessErr error
	processCompFailureErr error
	getSagaStateResult    *models.SagaState
	getSagaStateErr       error

	startSagaCalled          bool
	processStepSuccessCalled bool
	processStepFailureCalled bool
	processCompSuccessCalled bool
	processCompFailureCalled bool
}

func (m *mockOrchestrator) StartSaga(_ context.Context, _ *models.Order) error {
	m.startSagaCalled = true
	return m.startSagaErr
}

func (m *mockOrchestrator) ProcessStepSuccess(_ context.Context, _ uuid.UUID) error {
	m.processStepSuccessCalled = true
	return m.processStepSuccessErr
}

func (m *mockOrchestrator) ProcessStepFailure(_ context.Context, _ uuid.UUID) error {
	m.processStepFailureCalled = true
	return m.processStepFailureErr
}

func (m *mockOrchestrator) ProcessCompensationSuccess(_ context.Context, _ uuid.UUID) error {
	m.processCompSuccessCalled = true
	return m.processCompSuccessErr
}

func (m *mockOrchestrator) ProcessCompensationFailure(_ context.Context, _ uuid.UUID) error {
	m.processCompFailureCalled = true
	return m.processCompFailureErr
}

func (m *mockOrchestrator) GetSagaState(_ context.Context, _ uuid.UUID) (*models.SagaState, error) {
	return m.getSagaStateResult, m.getSagaStateErr
}

func (m *mockOrchestrator) RetryWorker(_ context.Context) {}

type mockHandlerSagaRepo struct {
	getByOrderIDResult *models.SagaState
	getByOrderIDErr    error
}

func (m *mockHandlerSagaRepo) Create(_ context.Context, _ *models.SagaState) error { return nil }
func (m *mockHandlerSagaRepo) GetByID(_ context.Context, _ uuid.UUID) (*models.SagaState, error) {
	return nil, nil
}
func (m *mockHandlerSagaRepo) GetByOrderID(_ context.Context, _ uuid.UUID) (*models.SagaState, error) {
	return m.getByOrderIDResult, m.getByOrderIDErr
}
func (m *mockHandlerSagaRepo) Update(_ context.Context, _ *models.SagaState) error { return nil }
func (m *mockHandlerSagaRepo) UpdateStatus(_ context.Context, _ uuid.UUID, _ models.SagaStatus) error {
	return nil
}
func (m *mockHandlerSagaRepo) UpdateStep(_ context.Context, _ uuid.UUID, _ models.SagaStep) error {
	return nil
}
func (m *mockHandlerSagaRepo) UpdateStepAndStatus(_ context.Context, _ uuid.UUID, _ models.SagaStep, _ models.SagaStatus) error {
	return nil
}
func (m *mockHandlerSagaRepo) Delete(_ context.Context, _ uuid.UUID) error { return nil }
func (m *mockHandlerSagaRepo) List(_ context.Context, _, _ int) ([]*models.SagaState, error) {
	return nil, nil
}
func (m *mockHandlerSagaRepo) GetByStatus(_ context.Context, _ models.SagaStatus, _, _ int) ([]*models.SagaState, error) {
	return nil, nil
}
func (m *mockHandlerSagaRepo) FindRetryable(_ context.Context, _ time.Time) ([]models.SagaState, error) {
	return nil, nil
}

type mockHandlerOrderRepo struct {
	getByPublicIDResult *models.Order
	getByPublicIDErr    error
}

func (m *mockHandlerOrderRepo) Create(_ context.Context, _ *models.Order) error { return nil }
func (m *mockHandlerOrderRepo) Update(_ context.Context, _ *models.Order) error { return nil }
func (m *mockHandlerOrderRepo) Delete(_ context.Context, _ uuid.UUID) error     { return nil }

func (m *mockHandlerOrderRepo) GetByID(_ context.Context, _ uuid.UUID) (*models.Order, error) {
	return nil, nil
}

func (m *mockHandlerOrderRepo) GetByPublicID(_ context.Context, _ string) (*models.Order, error) {
	return m.getByPublicIDResult, m.getByPublicIDErr
}

func (m *mockHandlerOrderRepo) GetByCustomerID(_ context.Context, _ string, _, _ int) ([]*models.Order, error) {
	return nil, nil
}

func (m *mockHandlerOrderRepo) UpdateStatus(_ context.Context, _ uuid.UUID, _ models.OrderStatus) error {
	return nil
}

func (m *mockHandlerOrderRepo) List(_ context.Context, _, _ int) ([]*models.Order, error) {
	return nil, nil
}

// ---- Helpers ----

func handlerTestConfig() *config.Config {
	return &config.Config{
		Topics: config.TopicsConfig{
			Commands: config.CommandTopics{
				Orders:       "orders.commands",
				Inventory:    "inventory.commands",
				Payment:      "payment.commands",
				Notification: "notification.commands",
			},
			Replies: config.ReplyTopics{
				Inventory:    "inventory.replies",
				Payment:      "payment.replies",
				Notification: "notification.replies",
			},
		},
	}
}

func makeMetadataHeader(t *testing.T, m kafka.RecordMetadata) kgo.RecordHeader {
	t.Helper()
	b, err := m.MarshalBinary()
	require.NoError(t, err)
	return kgo.RecordHeader{Key: "metadata", Value: b}
}

func makeReplyPayload(t *testing.T, v any) []byte {
	t.Helper()
	b, err := sonic.Marshal(v)
	require.NoError(t, err)
	return b
}

func newHandlerWithMocks(orch *mockOrchestrator, orderRepo *mockHandlerOrderRepo, sagaRepo ...*mockHandlerSagaRepo) *Handler {
	var sr *mockHandlerSagaRepo
	if len(sagaRepo) > 0 {
		sr = sagaRepo[0]
	} else {
		sr = &mockHandlerSagaRepo{getByOrderIDErr: errors.New("not found")}
	}
	return NewHandler(orch, orderRepo, sr, handlerTestConfig())
}

// ---- HandleRecord ----

func TestHandleRecord_RoutesToNewOrder(t *testing.T) {
	orch := &mockOrchestrator{}
	orderRepo := &mockHandlerOrderRepo{getByPublicIDErr: errors.New("not found")}
	h := newHandlerWithMocks(orch, orderRepo)

	order := models.Order{PublicID: "ORD-123", CustomerID: "cust-1", Status: models.OrderPending}
	payload, _ := sonic.Marshal(order)

	err := h.HandleRecord(context.Background(), &kgo.Record{
		Topic: "orders.commands",
		Value: payload,
	})

	require.NoError(t, err)
	assert.True(t, orch.startSagaCalled)
}

func TestHandleRecord_RoutesToReply(t *testing.T) {
	topics := []string{"inventory.replies", "payment.replies", "notification.replies"}
	eventTypes := []models.EventType{
		models.EventInventoryReserved,
		models.EventPaymentProcessed,
		models.EventNotificationSent,
	}
	replies := []any{
		&models.InventoryReply{Success: true},
		&models.PaymentReply{Success: true},
		&models.NotificationReply{Success: true},
	}

	for i, topic := range topics {
		t.Run(topic, func(t *testing.T) {
			sagaID := uuid.New()
			orch := &mockOrchestrator{
				getSagaStateResult: &models.SagaState{
					SagaID: sagaID,
					Status: models.SagaInProgress,
				},
			}
			h := newHandlerWithMocks(orch, &mockHandlerOrderRepo{})

			meta := kafka.RecordMetadata{
				EventType: eventTypes[i],
				SagaID:    sagaID,
				OrderID:   uuid.New(),
			}

			err := h.HandleRecord(context.Background(), &kgo.Record{
				Topic:   topic,
				Headers: []kgo.RecordHeader{makeMetadataHeader(t, meta)},
				Value:   makeReplyPayload(t, replies[i]),
			})

			require.NoError(t, err)
			assert.True(t, orch.processStepSuccessCalled)
		})
	}
}

func TestHandleRecord_UnexpectedTopic(t *testing.T) {
	h := newHandlerWithMocks(&mockOrchestrator{}, &mockHandlerOrderRepo{})

	err := h.HandleRecord(context.Background(), &kgo.Record{
		Topic: "unknown.topic",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected topic")
}

// ---- HandleNewOrder ----

func TestHandleNewOrder_Success(t *testing.T) {
	orch := &mockOrchestrator{}
	orderRepo := &mockHandlerOrderRepo{getByPublicIDErr: errors.New("not found")}
	h := newHandlerWithMocks(orch, orderRepo)

	order := models.Order{PublicID: "ORD-NEW", CustomerID: "cust-1"}
	payload, _ := sonic.Marshal(order)

	err := h.handleNewOrder(context.Background(), &kgo.Record{Value: payload})

	require.NoError(t, err)
	assert.True(t, orch.startSagaCalled)
}

func TestHandleNewOrder_DuplicateOrder(t *testing.T) {
	orch := &mockOrchestrator{}
	orderRepo := &mockHandlerOrderRepo{}
	sagaRepo := &mockHandlerSagaRepo{
		getByOrderIDResult: &models.SagaState{SagaID: uuid.New()},
		getByOrderIDErr:    nil,
	}
	h := newHandlerWithMocks(orch, orderRepo, sagaRepo)

	order := models.Order{PublicID: "ORD-DUP"}
	payload, _ := sonic.Marshal(order)

	err := h.handleNewOrder(context.Background(), &kgo.Record{Value: payload})

	require.NoError(t, err)
	assert.False(t, orch.startSagaCalled)
}

func TestHandleNewOrder_InvalidPayload(t *testing.T) {
	h := newHandlerWithMocks(&mockOrchestrator{}, &mockHandlerOrderRepo{})

	err := h.handleNewOrder(context.Background(), &kgo.Record{Value: []byte("not-json")})

	assert.Error(t, err)
}

func TestHandleNewOrder_StartSagaError(t *testing.T) {
	sagaErr := errors.New("saga creation failed")
	orch := &mockOrchestrator{startSagaErr: sagaErr}
	orderRepo := &mockHandlerOrderRepo{getByPublicIDErr: errors.New("not found")}
	h := newHandlerWithMocks(orch, orderRepo)

	order := models.Order{PublicID: "ORD-ERR", CustomerID: "cust-1"}
	payload, _ := sonic.Marshal(order)

	err := h.handleNewOrder(context.Background(), &kgo.Record{Value: payload})

	assert.ErrorIs(t, err, sagaErr)
}

// ---- HandleReply ----

func TestHandleReply_MissingMetadata(t *testing.T) {
	h := newHandlerWithMocks(&mockOrchestrator{}, &mockHandlerOrderRepo{})

	err := h.handleReply(context.Background(), &kgo.Record{
		Headers: []kgo.RecordHeader{},
		Value:   []byte("{}"),
	})

	assert.ErrorIs(t, err, errs.ErrMissingMetadata)
}

func TestHandleReply_InvalidMetadataJSON(t *testing.T) {
	h := newHandlerWithMocks(&mockOrchestrator{}, &mockHandlerOrderRepo{})

	err := h.handleReply(context.Background(), &kgo.Record{
		Headers: []kgo.RecordHeader{{Key: "metadata", Value: []byte("not-json")}},
		Value:   []byte("{}"),
	})

	assert.ErrorIs(t, err, errs.ErrMalformedPayload)
}

func TestHandleReply_SagaNotFound(t *testing.T) {
	sagaErr := errors.New("record not found")
	orch := &mockOrchestrator{getSagaStateErr: sagaErr}
	h := newHandlerWithMocks(orch, &mockHandlerOrderRepo{})

	meta := kafka.RecordMetadata{
		EventType: models.EventInventoryReserved,
		SagaID:    uuid.New(),
		OrderID:   uuid.New(),
	}

	err := h.handleReply(context.Background(), &kgo.Record{
		Headers: []kgo.RecordHeader{makeMetadataHeader(t, meta)},
		Value:   makeReplyPayload(t, &models.InventoryReply{Success: true}),
	})

	assert.ErrorIs(t, err, sagaErr)
}

func TestHandleReply_InProgressSuccess(t *testing.T) {
	sagaID := uuid.New()
	orch := &mockOrchestrator{
		getSagaStateResult: &models.SagaState{SagaID: sagaID, Status: models.SagaInProgress},
	}
	h := newHandlerWithMocks(orch, &mockHandlerOrderRepo{})

	meta := kafka.RecordMetadata{
		EventType: models.EventInventoryReserved,
		SagaID:    sagaID,
		OrderID:   uuid.New(),
	}

	err := h.handleReply(context.Background(), &kgo.Record{
		Headers: []kgo.RecordHeader{makeMetadataHeader(t, meta)},
		Value:   makeReplyPayload(t, &models.InventoryReply{Success: true}),
	})

	require.NoError(t, err)
	assert.True(t, orch.processStepSuccessCalled)
	assert.False(t, orch.processStepFailureCalled)
}

func TestHandleReply_InProgressFailure(t *testing.T) {
	sagaID := uuid.New()
	orch := &mockOrchestrator{
		getSagaStateResult: &models.SagaState{SagaID: sagaID, Status: models.SagaInProgress},
	}
	h := newHandlerWithMocks(orch, &mockHandlerOrderRepo{})

	meta := kafka.RecordMetadata{
		EventType: models.EventReserveInventoryFailed,
		SagaID:    sagaID,
		OrderID:   uuid.New(),
	}

	err := h.handleReply(context.Background(), &kgo.Record{
		Headers: []kgo.RecordHeader{makeMetadataHeader(t, meta)},
		Value:   makeReplyPayload(t, &models.InventoryReply{Success: false}),
	})

	require.NoError(t, err)
	assert.True(t, orch.processStepFailureCalled)
	assert.False(t, orch.processStepSuccessCalled)
}

func TestHandleReply_StartedSuccess(t *testing.T) {
	sagaID := uuid.New()
	orch := &mockOrchestrator{
		getSagaStateResult: &models.SagaState{SagaID: sagaID, Status: models.SagaStarted},
	}
	h := newHandlerWithMocks(orch, &mockHandlerOrderRepo{})

	meta := kafka.RecordMetadata{
		EventType: models.EventInventoryReserved,
		SagaID:    sagaID,
		OrderID:   uuid.New(),
	}

	err := h.handleReply(context.Background(), &kgo.Record{
		Headers: []kgo.RecordHeader{makeMetadataHeader(t, meta)},
		Value:   makeReplyPayload(t, &models.InventoryReply{Success: true}),
	})

	require.NoError(t, err)
	assert.True(t, orch.processStepSuccessCalled)
}

func TestHandleReply_CompensatingSuccess(t *testing.T) {
	sagaID := uuid.New()
	orch := &mockOrchestrator{
		getSagaStateResult: &models.SagaState{SagaID: sagaID, Status: models.SagaCompensating},
	}
	h := newHandlerWithMocks(orch, &mockHandlerOrderRepo{})

	meta := kafka.RecordMetadata{
		EventType: models.EventInventoryReleased,
		SagaID:    sagaID,
		OrderID:   uuid.New(),
	}

	err := h.handleReply(context.Background(), &kgo.Record{
		Headers: []kgo.RecordHeader{makeMetadataHeader(t, meta)},
		Value:   makeReplyPayload(t, &models.InventoryReply{Success: true}),
	})

	require.NoError(t, err)
	assert.True(t, orch.processCompSuccessCalled)
	assert.False(t, orch.processCompFailureCalled)
}

func TestHandleReply_CompensatingFailure(t *testing.T) {
	sagaID := uuid.New()
	orch := &mockOrchestrator{
		getSagaStateResult: &models.SagaState{SagaID: sagaID, Status: models.SagaCompensating},
	}
	h := newHandlerWithMocks(orch, &mockHandlerOrderRepo{})

	meta := kafka.RecordMetadata{
		EventType: models.EventReleaseInventoryFailed,
		SagaID:    sagaID,
		OrderID:   uuid.New(),
	}

	err := h.handleReply(context.Background(), &kgo.Record{
		Headers: []kgo.RecordHeader{makeMetadataHeader(t, meta)},
		Value:   makeReplyPayload(t, &models.InventoryReply{Success: false}),
	})

	require.NoError(t, err)
	assert.True(t, orch.processCompFailureCalled)
	assert.False(t, orch.processCompSuccessCalled)
}

func TestHandleReply_BackoffPeriod(t *testing.T) {
	sagaID := uuid.New()
	future := time.Now().Add(10 * time.Minute)
	orch := &mockOrchestrator{
		getSagaStateResult: &models.SagaState{
			SagaID:      sagaID,
			Status:      models.SagaCompensating,
			NextRetryAt: &future,
		},
	}
	h := newHandlerWithMocks(orch, &mockHandlerOrderRepo{})

	meta := kafka.RecordMetadata{
		EventType: models.EventInventoryReleased,
		SagaID:    sagaID,
		OrderID:   uuid.New(),
	}

	err := h.handleReply(context.Background(), &kgo.Record{
		Headers: []kgo.RecordHeader{makeMetadataHeader(t, meta)},
		Value:   makeReplyPayload(t, &models.InventoryReply{Success: true}),
	})

	require.NoError(t, err)
	// Should skip processing — no orchestrator methods called
	assert.False(t, orch.processCompSuccessCalled)
	assert.False(t, orch.processCompFailureCalled)
}

func TestHandleReply_UnexpectedSagaStatus(t *testing.T) {
	sagaID := uuid.New()
	orch := &mockOrchestrator{
		getSagaStateResult: &models.SagaState{SagaID: sagaID, Status: models.SagaCompleted},
	}
	h := newHandlerWithMocks(orch, &mockHandlerOrderRepo{})

	meta := kafka.RecordMetadata{
		EventType: models.EventInventoryReserved,
		SagaID:    sagaID,
		OrderID:   uuid.New(),
	}

	err := h.handleReply(context.Background(), &kgo.Record{
		Headers: []kgo.RecordHeader{makeMetadataHeader(t, meta)},
		Value:   makeReplyPayload(t, &models.InventoryReply{Success: true}),
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status")
}

func TestHandleReply_UnknownEventType(t *testing.T) {
	sagaID := uuid.New()
	orch := &mockOrchestrator{
		getSagaStateResult: &models.SagaState{SagaID: sagaID, Status: models.SagaInProgress},
	}
	h := newHandlerWithMocks(orch, &mockHandlerOrderRepo{})

	meta := kafka.RecordMetadata{
		EventType: models.EventType("TOTALLY_UNKNOWN"),
		SagaID:    sagaID,
		OrderID:   uuid.New(),
	}

	err := h.handleReply(context.Background(), &kgo.Record{
		Headers: []kgo.RecordHeader{makeMetadataHeader(t, meta)},
		Value:   []byte(`{"success": true}`),
	})

	assert.ErrorIs(t, err, errs.ErrUnknownEvent)
}

// ---- unmarshalReplyByEventType ----

func TestUnmarshalReply_InventoryEvents(t *testing.T) {
	h := newHandlerWithMocks(&mockOrchestrator{}, &mockHandlerOrderRepo{})

	events := []models.EventType{
		models.EventInventoryReserved,
		models.EventInventoryReleased,
		models.EventReserveInventoryFailed,
		models.EventReleaseInventoryFailed,
	}

	payload := makeReplyPayload(t, &models.InventoryReply{Success: true, Message: "ok"})

	for _, ev := range events {
		t.Run(string(ev), func(t *testing.T) {
			reply, err := h.unmarshalReplyByEventType(ev, payload)
			require.NoError(t, err)
			ir, ok := reply.(*models.InventoryReply)
			require.True(t, ok)
			assert.True(t, ir.Success)
		})
	}
}

func TestUnmarshalReply_PaymentEvents(t *testing.T) {
	h := newHandlerWithMocks(&mockOrchestrator{}, &mockHandlerOrderRepo{})

	events := []models.EventType{
		models.EventPaymentProcessed,
		models.EventPaymentFailed,
	}

	payload := makeReplyPayload(t, &models.PaymentReply{Success: true, PaymentID: "pay-123"})

	for _, ev := range events {
		t.Run(string(ev), func(t *testing.T) {
			reply, err := h.unmarshalReplyByEventType(ev, payload)
			require.NoError(t, err)
			pr, ok := reply.(*models.PaymentReply)
			require.True(t, ok)
			assert.True(t, pr.Success)
		})
	}
}

func TestUnmarshalReply_NotificationEvents(t *testing.T) {
	h := newHandlerWithMocks(&mockOrchestrator{}, &mockHandlerOrderRepo{})

	events := []models.EventType{
		models.EventNotificationSent,
		models.EventNotificationFailed,
	}

	payload := makeReplyPayload(t, &models.NotificationReply{Success: true})

	for _, ev := range events {
		t.Run(string(ev), func(t *testing.T) {
			reply, err := h.unmarshalReplyByEventType(ev, payload)
			require.NoError(t, err)
			nr, ok := reply.(*models.NotificationReply)
			require.True(t, ok)
			assert.True(t, nr.Success)
		})
	}
}

func TestUnmarshalReply_InvalidPayload(t *testing.T) {
	h := newHandlerWithMocks(&mockOrchestrator{}, &mockHandlerOrderRepo{})

	_, err := h.unmarshalReplyByEventType(models.EventInventoryReserved, []byte("bad"))

	assert.Error(t, err)
}

func TestUnmarshalReply_UnknownEvent(t *testing.T) {
	h := newHandlerWithMocks(&mockOrchestrator{}, &mockHandlerOrderRepo{})

	_, err := h.unmarshalReplyByEventType(models.EventType("NOPE"), []byte("{}"))

	assert.ErrorIs(t, err, errs.ErrUnknownEvent)
}

// ---- isSuccessReply ----

func TestIsSuccessReply(t *testing.T) {
	tests := []struct {
		name     string
		reply    any
		expected bool
	}{
		{"InventoryReply_Success", &models.InventoryReply{Success: true}, true},
		{"InventoryReply_Failure", &models.InventoryReply{Success: false}, false},
		{"PaymentReply_Success", &models.PaymentReply{Success: true}, true},
		{"PaymentReply_Failure", &models.PaymentReply{Success: false}, false},
		{"NotificationReply_Success", &models.NotificationReply{Success: true}, true},
		{"NotificationReply_Failure", &models.NotificationReply{Success: false}, false},
		{"UnknownType", "not a reply", false},
		{"Nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isSuccessReply(tt.reply))
		})
	}
}
