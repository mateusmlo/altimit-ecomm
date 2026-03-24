//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSagaCreate(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewSagaRepository(testDB)
	ctx := context.Background()

	saga := newTestSagaState(uuid.New())
	saga.SagaID = uuid.Nil

	err := repo.Create(ctx, saga)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, saga.SagaID)
}

func TestSagaCreate_WithJSONBPayload(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewSagaRepository(testDB)
	ctx := context.Background()

	payload := sonic.NoCopyRawMessage(`{"order_id": "abc-123", "items": [{"product_id": "p1", "qty": 2}]}`)
	saga := newTestSagaState(uuid.New(), func(s *models.SagaState) {
		s.Payload = payload
	})

	require.NoError(t, repo.Create(ctx, saga))

	fetched, err := repo.GetByID(ctx, saga.SagaID)
	require.NoError(t, err)
	assert.JSONEq(t, string(payload), string(fetched.Payload))
}

func TestSagaGetByID_Found(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewSagaRepository(testDB)
	ctx := context.Background()

	saga := newTestSagaState(uuid.New())
	require.NoError(t, repo.Create(ctx, saga))

	fetched, err := repo.GetByID(ctx, saga.SagaID)
	require.NoError(t, err)
	assert.Equal(t, saga.SagaID, fetched.SagaID)
	assert.Equal(t, saga.Status, fetched.Status)
}

func TestSagaGetByID_NotFound(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewSagaRepository(testDB)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, uuid.New())

	require.Error(t, err)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestSagaGetByOrderID(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewSagaRepository(testDB)
	ctx := context.Background()

	orderID := uuid.New()
	saga := newTestSagaState(orderID)
	require.NoError(t, repo.Create(ctx, saga))

	fetched, err := repo.GetByOrderID(ctx, orderID)
	require.NoError(t, err)
	assert.Equal(t, orderID, fetched.OrderID)
}

func TestSagaUpdate(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewSagaRepository(testDB)
	ctx := context.Background()

	saga := newTestSagaState(uuid.New())
	require.NoError(t, repo.Create(ctx, saga))

	saga.CurrentStep = models.StepProcessPayment
	err := repo.Update(ctx, saga)
	require.NoError(t, err)

	fetched, err := repo.GetByID(ctx, saga.SagaID)
	require.NoError(t, err)
	assert.Equal(t, models.StepProcessPayment, fetched.CurrentStep)
}

func TestSagaUpdateStatus(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewSagaRepository(testDB)
	ctx := context.Background()

	saga := newTestSagaState(uuid.New())
	require.NoError(t, repo.Create(ctx, saga))

	err := repo.UpdateStatus(ctx, saga.SagaID, models.SagaCompleted)
	require.NoError(t, err)

	fetched, err := repo.GetByID(ctx, saga.SagaID)
	require.NoError(t, err)
	assert.Equal(t, models.SagaCompleted, fetched.Status)
}

func TestSagaUpdateStep(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewSagaRepository(testDB)
	ctx := context.Background()

	saga := newTestSagaState(uuid.New())
	require.NoError(t, repo.Create(ctx, saga))

	err := repo.UpdateStep(ctx, saga.SagaID, models.StepSendNotification)
	require.NoError(t, err)

	fetched, err := repo.GetByID(ctx, saga.SagaID)
	require.NoError(t, err)
	assert.Equal(t, models.StepSendNotification, fetched.CurrentStep)
}

func TestSagaUpdateStepAndStatus(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewSagaRepository(testDB)
	ctx := context.Background()

	saga := newTestSagaState(uuid.New())
	require.NoError(t, repo.Create(ctx, saga))

	err := repo.UpdateStepAndStatus(ctx, saga.SagaID, models.StepProcessPayment, models.SagaInProgress)
	require.NoError(t, err)

	fetched, err := repo.GetByID(ctx, saga.SagaID)
	require.NoError(t, err)
	assert.Equal(t, models.StepProcessPayment, fetched.CurrentStep)
	assert.Equal(t, models.SagaInProgress, fetched.Status)
}

func TestSagaDelete(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewSagaRepository(testDB)
	ctx := context.Background()

	saga := newTestSagaState(uuid.New())
	require.NoError(t, repo.Create(ctx, saga))

	err := repo.Delete(ctx, saga.SagaID)
	require.NoError(t, err)

	_, err = repo.GetByID(ctx, saga.SagaID)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestSagaList_Pagination(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewSagaRepository(testDB)
	ctx := context.Background()

	for i := 0; i < 8; i++ {
		require.NoError(t, repo.Create(ctx, newTestSagaState(uuid.New())))
	}

	tests := []struct {
		name          string
		limit, offset int
		expectedCount int
	}{
		{"first page", 5, 0, 5},
		{"second page", 5, 5, 3},
		{"beyond data", 5, 10, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := repo.List(ctx, tt.limit, tt.offset)
			require.NoError(t, err)
			assert.Len(t, result, tt.expectedCount)
		})
	}
}

func TestSagaGetByStatus(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewSagaRepository(testDB)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		require.NoError(t, repo.Create(ctx, newTestSagaState(uuid.New())))
	}
	for i := 0; i < 2; i++ {
		s := newTestSagaState(uuid.New(), func(s *models.SagaState) {
			s.Status = models.SagaCompleted
		})
		require.NoError(t, repo.Create(ctx, s))
	}

	started, err := repo.GetByStatus(ctx, models.SagaStarted, 10, 0)
	require.NoError(t, err)
	assert.Len(t, started, 3)

	completed, err := repo.GetByStatus(ctx, models.SagaCompleted, 10, 0)
	require.NoError(t, err)
	assert.Len(t, completed, 2)
}
