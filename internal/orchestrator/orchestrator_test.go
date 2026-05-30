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
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- Mocks ----

type mockSagaRepo struct {
	createErr              error
	getByIDResult          *models.SagaState
	getByIDErr             error
	updateErr              error
	updateStatusErr        error
	updateStepAndStatusErr error
}

func (m *mockSagaRepo) Create(_ context.Context, s *models.SagaState) error {
	if m.createErr != nil {
		return m.createErr
	}
	s.SagaID = uuid.New()
	return nil
}

func (m *mockSagaRepo) GetByID(_ context.Context, _ uuid.UUID) (*models.SagaState, error) {
	return m.getByIDResult, m.getByIDErr
}

func (m *mockSagaRepo) GetByOrderID(_ context.Context, _ uuid.UUID) (*models.SagaState, error) {
	return nil, nil
}

func (m *mockSagaRepo) Update(_ context.Context, _ *models.SagaState) error {
	return m.updateErr
}

func (m *mockSagaRepo) UpdateStatus(_ context.Context, _ uuid.UUID, _ models.SagaStatus) error {
	return m.updateStatusErr
}

func (m *mockSagaRepo) UpdateStep(_ context.Context, _ uuid.UUID, _ models.SagaStep) error {
	return nil
}

func (m *mockSagaRepo) UpdateStepAndStatus(_ context.Context, _ uuid.UUID, _ models.SagaStep, _ models.SagaStatus) error {
	return m.updateStepAndStatusErr
}

func (m *mockSagaRepo) Delete(_ context.Context, _ uuid.UUID) error { return nil }

func (m *mockSagaRepo) List(_ context.Context, _, _ int) ([]*models.SagaState, error) {
	return nil, nil
}

func (m *mockSagaRepo) GetByStatus(_ context.Context, _ models.SagaStatus, _, _ int) ([]*models.SagaState, error) {
	return nil, nil
}

func (m *mockSagaRepo) FindRetryable(_ context.Context, _ time.Time) ([]models.SagaState, error) {
	return nil, nil
}

type mockOrderRepo struct {
	getByIDResult    *models.Order
	getByIDErr       error
	getByPublicIDErr error
	updateStatusErr  error
}

func (m *mockOrderRepo) Create(_ context.Context, _ *models.Order) error { return nil }
func (m *mockOrderRepo) Update(_ context.Context, _ *models.Order) error { return nil }
func (m *mockOrderRepo) Delete(_ context.Context, _ uuid.UUID) error     { return nil }

func (m *mockOrderRepo) GetByID(_ context.Context, _ uuid.UUID) (*models.Order, error) {
	return m.getByIDResult, m.getByIDErr
}

func (m *mockOrderRepo) GetByPublicID(_ context.Context, _ string) (*models.Order, error) {
	return nil, m.getByPublicIDErr
}

func (m *mockOrderRepo) GetByCustomerID(_ context.Context, _ string, _, _ int) ([]*models.Order, error) {
	return nil, nil
}

func (m *mockOrderRepo) UpdateStatus(_ context.Context, _ uuid.UUID, _ models.OrderStatus) error {
	return m.updateStatusErr
}

func (m *mockOrderRepo) List(_ context.Context, _, _ int) ([]*models.Order, error) {
	return nil, nil
}

type mockPaymentRepo struct {
	getByOrderIDResult *models.Payment
	getByOrderIDErr    error
}

func (m *mockPaymentRepo) Create(_ context.Context, _ *models.Payment) error { return nil }
func (m *mockPaymentRepo) GetByID(_ context.Context, _ uuid.UUID) (*models.Payment, error) {
	return nil, nil
}
func (m *mockPaymentRepo) GetByOrderID(_ context.Context, _ uuid.UUID) (*models.Payment, error) {
	return m.getByOrderIDResult, m.getByOrderIDErr
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

func newTestPayment(orderID uuid.UUID) *models.Payment {
	return &models.Payment{
		ID:              uuid.New(),
		OrderID:         orderID,
		Amount:          99.99,
		Currency:        "USD",
		Status:          models.PaymentPending,
		PaymentIntentID: "pi_test_123",
	}
}

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

// ---- Helpers ----

func testConfig() *config.Config {
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
			DLQ: config.DLQTopics{
				Orders: "orders.dlq",
			},
		},
	}
}

func newTestOrder() *models.Order {
	return &models.Order{
		ID:          uuid.New(),
		PublicID:    "ORD-" + uuid.NewString()[:8],
		CustomerID:  "cust-123",
		TotalAmount: 99.99,
		Currency:    "USD",
		Status:      models.OrderPending,
		Items: []models.OrderItem{
			{
				ID:       uuid.New(),
				ItemID:   uuid.New(),
				Quantity: 2,
				Price:    49.995,
			},
		},
	}
}

func newTestSaga(orderID uuid.UUID, step models.SagaStep, status models.SagaStatus) *models.SagaState {
	return &models.SagaState{
		SagaID:      uuid.New(),
		OrderID:     orderID,
		Status:      status,
		CurrentStep: step,
		StartedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
}

// ---- StartSaga ----

func TestStartSaga_Success(t *testing.T) {
	pub := &mockPublisher{}
	order := newTestOrder()
	orch := NewOrchestrator(
		&mockSagaRepo{},
		&mockOrderRepo{},
		pub,
		testConfig(),
	)

	err := orch.StartSaga(context.Background(), order)

	require.NoError(t, err)
	assert.Equal(t, "inventory.commands", pub.lastTopic)
	assert.Equal(t, models.EventReserveInventory, pub.lastEvent.Event)
	assert.Equal(t, order.ID, pub.lastEvent.OrderID)
}

func TestStartSaga_CreateSagaError(t *testing.T) {
	dbErr := errors.New("db connection lost")
	orch := NewOrchestrator(
		&mockSagaRepo{createErr: dbErr},
		&mockOrderRepo{},
		&mockPublisher{},
		testConfig(),
	)

	err := orch.StartSaga(context.Background(), newTestOrder())

	assert.ErrorIs(t, err, dbErr)
}

func TestStartSaga_UpdateOrderStatusError(t *testing.T) {
	dbErr := errors.New("update failed")
	orch := NewOrchestrator(
		&mockSagaRepo{},
		&mockOrderRepo{updateStatusErr: dbErr},
		&mockPublisher{},
		testConfig(),
	)

	err := orch.StartSaga(context.Background(), newTestOrder())

	assert.ErrorIs(t, err, dbErr)
}

func TestStartSaga_PublishError(t *testing.T) {
	pubErr := errors.New("kafka unavailable")
	order := newTestOrder()
	orch := NewOrchestrator(
		&mockSagaRepo{},
		&mockOrderRepo{},
		&mockPublisher{publishErr: pubErr},
		testConfig(),
	)

	err := orch.StartSaga(context.Background(), order)

	assert.ErrorIs(t, err, pubErr)
}

// ---- GetSagaState ----

func TestGetSagaState_Success(t *testing.T) {
	sagaID := uuid.New()
	expected := &models.SagaState{SagaID: sagaID, Status: models.SagaStarted}
	orch := NewOrchestrator(
		&mockSagaRepo{getByIDResult: expected},
		&mockOrderRepo{},
		&mockPublisher{},
		testConfig(),
	)

	result, err := orch.GetSagaState(context.Background(), sagaID)

	require.NoError(t, err)
	assert.Equal(t, expected, result)
}

func TestGetSagaState_NotFound(t *testing.T) {
	notFoundErr := errors.New("record not found")
	orch := NewOrchestrator(
		&mockSagaRepo{getByIDErr: notFoundErr},
		&mockOrderRepo{},
		&mockPublisher{},
		testConfig(),
	)

	result, err := orch.GetSagaState(context.Background(), uuid.New())

	assert.Nil(t, result)
	assert.ErrorIs(t, err, notFoundErr)
}

// ---- ProcessStepSuccess ----

func TestProcessStepSuccess_AdvancesToNextStep(t *testing.T) {
	order := newTestOrder()
	saga := newTestSaga(order.ID, models.StepReserveInventory, models.SagaInProgress)
	pub := &mockPublisher{}

	orch := NewOrchestrator(
		&mockSagaRepo{getByIDResult: saga},
		&mockOrderRepo{getByIDResult: order},
		pub,
		testConfig(),
	)

	err := orch.ProcessStepSuccess(context.Background(), saga.SagaID)

	require.NoError(t, err)
	assert.Equal(t, "payment.commands", pub.lastTopic)
	assert.Equal(t, models.EventProcessPayment, pub.lastEvent.Event)
}

func TestProcessStepSuccess_CompletesWorkflow(t *testing.T) {
	order := newTestOrder()
	saga := newTestSaga(order.ID, models.StepSendNotification, models.SagaInProgress)
	pub := &mockPublisher{}

	orch := NewOrchestrator(
		&mockSagaRepo{getByIDResult: saga},
		&mockOrderRepo{getByIDResult: order},
		pub,
		testConfig(),
	)

	err := orch.ProcessStepSuccess(context.Background(), saga.SagaID)

	require.NoError(t, err)
	// No event published when workflow completes
	assert.Empty(t, pub.lastTopic)
}

func TestProcessStepSuccess_SagaNotFound(t *testing.T) {
	sagaErr := errors.New("record not found")
	orch := NewOrchestrator(
		&mockSagaRepo{getByIDErr: sagaErr},
		&mockOrderRepo{},
		&mockPublisher{},
		testConfig(),
	)

	err := orch.ProcessStepSuccess(context.Background(), uuid.New())

	assert.ErrorIs(t, err, sagaErr)
}

func TestProcessStepSuccess_UpdateStepError(t *testing.T) {
	order := newTestOrder()
	saga := newTestSaga(order.ID, models.StepReserveInventory, models.SagaInProgress)
	dbErr := errors.New("update failed")

	orch := NewOrchestrator(
		&mockSagaRepo{getByIDResult: saga, updateStepAndStatusErr: dbErr},
		&mockOrderRepo{getByIDResult: order},
		&mockPublisher{},
		testConfig(),
	)

	err := orch.ProcessStepSuccess(context.Background(), saga.SagaID)

	assert.ErrorIs(t, err, dbErr)
}

// ---- ProcessStepFailure ----

func TestProcessStepFailure_NoCompletedSteps(t *testing.T) {
	order := newTestOrder()
	// Fail at first step — nothing to compensate
	saga := newTestSaga(order.ID, models.StepReserveInventory, models.SagaInProgress)

	orch := NewOrchestrator(
		&mockSagaRepo{getByIDResult: saga},
		&mockOrderRepo{getByIDResult: order},
		&mockPublisher{},
		testConfig(),
	)

	err := orch.ProcessStepFailure(context.Background(), saga.SagaID)

	require.NoError(t, err)
}

func TestProcessStepFailure_FailAtPayment_CompensatesInventory(t *testing.T) {
	order := newTestOrder()
	// Fail at payment — ProcessPayment has CompensationStep=StepCompensateInventory
	saga := newTestSaga(order.ID, models.StepProcessPayment, models.SagaInProgress)
	pub := &mockPublisher{}

	orch := NewOrchestrator(
		&mockSagaRepo{getByIDResult: saga},
		&mockOrderRepo{getByIDResult: order},
		pub,
		testConfig(),
	)

	err := orch.ProcessStepFailure(context.Background(), saga.SagaID)

	require.NoError(t, err)
	assert.Equal(t, models.EventReleaseInventory, pub.lastEvent.Event)
}

func TestProcessStepFailure_FailAtNotification_CompensatesPayment(t *testing.T) {
	order := newTestOrder()
	// Fail at notification — SendNotification has CompensationStep=StepCompensatePayment
	saga := newTestSaga(order.ID, models.StepSendNotification, models.SagaInProgress)
	pub := &mockPublisher{}

	orch := NewOrchestrator(
		&mockSagaRepo{getByIDResult: saga},
		&mockOrderRepo{getByIDResult: order},
		pub,
		testConfig(),
	)

	err := orch.ProcessStepFailure(context.Background(), saga.SagaID)

	require.NoError(t, err)
	// First compensation found walking backward is SendNotification's: CompensatePayment
	assert.Equal(t, models.EventRefundPayment, pub.lastEvent.Event)
}

func TestProcessStepFailure_SagaNotFound(t *testing.T) {
	sagaErr := errors.New("record not found")
	orch := NewOrchestrator(
		&mockSagaRepo{getByIDErr: sagaErr},
		&mockOrderRepo{},
		&mockPublisher{},
		testConfig(),
	)

	err := orch.ProcessStepFailure(context.Background(), uuid.New())

	assert.ErrorIs(t, err, sagaErr)
}

// ---- ProcessCompensationSuccess ----

func TestProcessCompensationSuccess_AllCompensated(t *testing.T) {
	order := newTestOrder()
	// Currently compensating inventory (the only compensation with no prior compensation)
	saga := newTestSaga(order.ID, models.StepCompensateInventory, models.SagaCompensating)

	orch := NewOrchestrator(
		&mockSagaRepo{getByIDResult: saga},
		&mockOrderRepo{getByIDResult: order},
		&mockPublisher{},
		testConfig(),
	)

	err := orch.ProcessCompensationSuccess(context.Background(), saga.SagaID)

	require.NoError(t, err)
}

func TestProcessCompensationSuccess_MoreStepsToCompensate(t *testing.T) {
	order := newTestOrder()
	// Just finished compensating payment, should now compensate inventory
	saga := newTestSaga(order.ID, models.StepCompensatePayment, models.SagaCompensating)
	pub := &mockPublisher{}

	orch := NewOrchestrator(
		&mockSagaRepo{getByIDResult: saga},
		&mockOrderRepo{getByIDResult: order},
		pub,
		testConfig(),
	)

	err := orch.ProcessCompensationSuccess(context.Background(), saga.SagaID)

	require.NoError(t, err)
	assert.Equal(t, models.EventReleaseInventory, pub.lastEvent.Event)
}

func TestProcessCompensationSuccess_SagaNotFound(t *testing.T) {
	sagaErr := errors.New("record not found")
	orch := NewOrchestrator(
		&mockSagaRepo{getByIDErr: sagaErr},
		&mockOrderRepo{},
		&mockPublisher{},
		testConfig(),
	)

	err := orch.ProcessCompensationSuccess(context.Background(), uuid.New())

	assert.ErrorIs(t, err, sagaErr)
}

// ---- ProcessCompensationFailure ----

func TestProcessCompensationFailure_Retries(t *testing.T) {
	order := newTestOrder()
	saga := newTestSaga(order.ID, models.StepCompensateInventory, models.SagaCompensating)
	saga.CompensationRetries = 0

	orch := NewOrchestrator(
		&mockSagaRepo{getByIDResult: saga},
		&mockOrderRepo{getByIDResult: order},
		&mockPublisher{},
		testConfig(),
	)

	err := orch.ProcessCompensationFailure(context.Background(), saga.SagaID)

	require.NoError(t, err)
	assert.Equal(t, 1, saga.CompensationRetries)
	assert.NotNil(t, saga.NextRetryAt)
	assert.True(t, saga.NextRetryAt.After(time.Now()))
}

func TestProcessCompensationFailure_ExceedsMaxRetries(t *testing.T) {
	order := newTestOrder()
	saga := newTestSaga(order.ID, models.StepCompensateInventory, models.SagaCompensating)
	saga.CompensationRetries = MaxCompensationRetries - 1

	orch := NewOrchestrator(
		&mockSagaRepo{getByIDResult: saga},
		&mockOrderRepo{getByIDResult: order},
		&mockPublisher{},
		testConfig(),
	)

	err := orch.ProcessCompensationFailure(context.Background(), saga.SagaID)

	assert.ErrorIs(t, err, errs.ErrCompensationFailed)
}

func TestProcessCompensationFailure_SagaNotFound(t *testing.T) {
	sagaErr := errors.New("record not found")
	orch := NewOrchestrator(
		&mockSagaRepo{getByIDErr: sagaErr},
		&mockOrderRepo{},
		&mockPublisher{},
		testConfig(),
	)

	err := orch.ProcessCompensationFailure(context.Background(), uuid.New())

	assert.ErrorIs(t, err, sagaErr)
}

func TestProcessCompensationFailure_ExceedsMaxRetries_PublishesToDLQ(t *testing.T) {
	order := newTestOrder()
	saga := newTestSaga(order.ID, models.StepCompensateInventory, models.SagaCompensating)
	saga.CompensationRetries = MaxCompensationRetries - 1
	pub := &mockPublisher{}

	orch := NewOrchestrator(
		&mockSagaRepo{getByIDResult: saga},
		&mockOrderRepo{getByIDResult: order},
		pub,
		testConfig(),
	)

	err := orch.ProcessCompensationFailure(context.Background(), saga.SagaID)

	require.ErrorIs(t, err, errs.ErrCompensationFailed)
	assert.Equal(t, "orders.dlq", pub.lastTopic)
	assert.Equal(t, models.EventCompensationFailed, pub.lastEvent.Event)
	assert.Equal(t, saga.SagaID, pub.lastEvent.SagaID)
	assert.Equal(t, order.ID, pub.lastEvent.OrderID)

	var payload models.CompensationFailedPayload
	require.NoError(t, sonic.Unmarshal(pub.lastEvent.Payload, &payload))
	assert.Equal(t, saga.SagaID, payload.SagaID)
	assert.Equal(t, order.ID, payload.OrderID)
	assert.Equal(t, string(models.StepCompensateInventory), payload.FailedStep)
	assert.Equal(t, MaxCompensationRetries, payload.Retries)
	assert.Equal(t, errs.ErrCompensationFailed.Error(), payload.Reason)
}

func TestProcessCompensationFailure_ExceedsMaxRetries_DLQPublishFailure_StillReturnsError(t *testing.T) {
	order := newTestOrder()
	saga := newTestSaga(order.ID, models.StepCompensateInventory, models.SagaCompensating)
	saga.CompensationRetries = MaxCompensationRetries - 1

	orch := NewOrchestrator(
		&mockSagaRepo{getByIDResult: saga},
		&mockOrderRepo{getByIDResult: order},
		&mockPublisher{publishErr: errors.New("kafka unavailable")},
		testConfig(),
	)

	err := orch.ProcessCompensationFailure(context.Background(), saga.SagaID)

	// DLQ is best-effort — a publish failure must not suppress the real error.
	assert.ErrorIs(t, err, errs.ErrCompensationFailed)
}

// ---- Helper functions ----

func TestFindStepIndex(t *testing.T) {
	workflow := models.GetOrderWorkflow()

	tests := []struct {
		name     string
		step     models.SagaStep
		expected int
	}{
		{"ReserveInventory", models.StepReserveInventory, 0},
		{"ProcessPayment", models.StepProcessPayment, 1},
		{"SendNotification", models.StepSendNotification, 2},
		{"UnknownStep", models.SagaStep("UNKNOWN"), -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, findStepIndex(workflow, tt.step))
		})
	}
}

func TestFindCompensationStepIndex(t *testing.T) {
	workflow := models.GetOrderWorkflow()

	tests := []struct {
		name     string
		step     models.SagaStep
		expected int
	}{
		{"CompensateInventory_FoundInPaymentStep", models.StepCompensateInventory, 1},
		{"CompensatePayment_FoundInNotificationStep", models.StepCompensatePayment, 2},
		{"Unknown_NotFound", models.SagaStep("UNKNOWN"), -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, findCompensationStepIndex(workflow, tt.step))
		})
	}
}

func TestFindNextCompensationStep(t *testing.T) {
	workflow := models.GetOrderWorkflow()

	t.Run("FromNotificationStep_FindsPaymentCompensation", func(t *testing.T) {
		// Index 2 is SendNotification, look backward from there
		result := findNextCompensationStep(workflow, 2)
		require.NotNil(t, result)
		assert.Equal(t, models.StepProcessPayment, result.Step)
	})

	t.Run("FromPaymentStep_NoMoreCompensations", func(t *testing.T) {
		// Index 1 is ProcessPayment, look backward — ReserveInventory has nil CompensationStep
		result := findNextCompensationStep(workflow, 1)
		assert.Nil(t, result)
	})

	t.Run("FromFirstStep_NoCompensations", func(t *testing.T) {
		result := findNextCompensationStep(workflow, 0)
		assert.Nil(t, result)
	})
}

func TestOrderItemsToInventoryProducts(t *testing.T) {
	itemID := uuid.New()
	items := []models.OrderItem{
		{ItemID: itemID, Quantity: 3},
		{ItemID: uuid.New(), Quantity: 1},
	}

	prods := orderItemsToInventoryProducts(items)

	require.Len(t, prods, 2)
	assert.Equal(t, itemID, prods[0].ProductID)
	assert.Equal(t, 3, prods[0].Quantity)
}

func TestOrderItemsToInventoryProducts_Empty(t *testing.T) {
	prods := orderItemsToInventoryProducts(nil)
	assert.Nil(t, prods)
}

func TestBuildCommandForStep_AllSteps(t *testing.T) {
	order := newTestOrder()
	o := &orchestrator{}

	tests := []struct {
		name    string
		step    models.SagaStep
		cmdType string
	}{
		{"ReserveInventory", models.StepReserveInventory, "*models.ReserveInventoryCommand"},
		{"ProcessPayment", models.StepProcessPayment, "*models.ProcessPaymentCommand"},
		{"SendNotification", models.StepSendNotification, "*models.SendNotificationCommand"},
		{"CompensateInventory", models.StepCompensateInventory, "*models.ReleaseInventoryCommand"},
		{"CompensatePayment", models.StepCompensatePayment, "*models.RefundPaymentCommand"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := o.buildCommandForStep(tt.step, order)
			require.NoError(t, err)
			assert.NotNil(t, cmd)
		})
	}
}

func TestBuildCommandForStep_UnknownStep(t *testing.T) {
	order := newTestOrder()
	o := &orchestrator{}

	cmd, err := o.buildCommandForStep(models.SagaStep("UNKNOWN"), order)

	assert.Nil(t, cmd)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown step")
}

func TestCalculateBackoff(t *testing.T) {
	t.Run("FirstAttempt", func(t *testing.T) {
		b := calculateBackoff(1)
		// InitialBackoff is 1s, with ±20% jitter → between 0.8s and 1.2s
		assert.GreaterOrEqual(t, b, 800*time.Millisecond)
		assert.LessOrEqual(t, b, 1200*time.Millisecond)
	})

	t.Run("SecondAttempt", func(t *testing.T) {
		b := calculateBackoff(2)
		// 2s base with ±20% jitter → between 1.6s and 2.4s
		assert.GreaterOrEqual(t, b, 1600*time.Millisecond)
		assert.LessOrEqual(t, b, 2400*time.Millisecond)
	})

	t.Run("HighAttempt_CapsAtMaxBackoff", func(t *testing.T) {
		b := calculateBackoff(100)
		// Should be capped at MaxBackoff (60s) + jitter
		assert.LessOrEqual(t, b, MaxBackoff+MaxBackoff/5)
	})
}
