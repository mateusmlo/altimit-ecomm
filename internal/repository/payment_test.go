//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPaymentCreate(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewPaymentRepository(testDB)
	ctx := context.Background()

	payment := newTestPayment(uuid.New())
	payment.ID = uuid.Nil

	err := repo.Create(ctx, payment)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, payment.ID)
}

func TestPaymentGetByID_Found(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewPaymentRepository(testDB)
	ctx := context.Background()

	payment := newTestPayment(uuid.New())
	require.NoError(t, testDB.Create(payment).Error)

	fetched, err := repo.GetByID(ctx, payment.ID)
	require.NoError(t, err)
	assert.Equal(t, payment.ID, fetched.ID)
	assert.InDelta(t, payment.Amount, fetched.Amount, 0.01)
}

func TestPaymentGetByID_NotFound(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewPaymentRepository(testDB)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, uuid.New())

	require.Error(t, err)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestPaymentGetByOrderID(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewPaymentRepository(testDB)
	ctx := context.Background()

	orderID := uuid.New()
	payment := newTestPayment(orderID)
	require.NoError(t, testDB.Create(payment).Error)

	fetched, err := repo.GetByOrderID(ctx, orderID)
	require.NoError(t, err)
	assert.Equal(t, orderID, fetched.OrderID)
}

func TestPaymentUpdate(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewPaymentRepository(testDB)
	ctx := context.Background()

	payment := newTestPayment(uuid.New())
	require.NoError(t, testDB.Create(payment).Error)

	payment.Amount = 199.99
	err := repo.Update(ctx, payment)
	require.NoError(t, err)

	fetched, err := repo.GetByID(ctx, payment.ID)
	require.NoError(t, err)
	assert.InDelta(t, 199.99, fetched.Amount, 0.01)
}

func TestPaymentUpdateStatus(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewPaymentRepository(testDB)
	ctx := context.Background()

	payment := newTestPayment(uuid.New())
	require.NoError(t, testDB.Create(payment).Error)

	err := repo.UpdateStatus(ctx, payment.ID, models.PaymentCompleted)
	require.NoError(t, err)

	fetched, err := repo.GetByID(ctx, payment.ID)
	require.NoError(t, err)
	assert.Equal(t, models.PaymentCompleted, fetched.Status)
}

func TestPaymentDelete(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewPaymentRepository(testDB)
	ctx := context.Background()

	payment := newTestPayment(uuid.New())
	require.NoError(t, testDB.Create(payment).Error)

	err := repo.Delete(ctx, payment.ID)
	require.NoError(t, err)

	_, err = repo.GetByID(ctx, payment.ID)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestPaymentList_Pagination(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewPaymentRepository(testDB)
	ctx := context.Background()

	for range 7 {
		require.NoError(t, testDB.Create(newTestPayment(uuid.New())).Error)
	}

	tests := []struct {
		name          string
		limit, offset int
		expectedCount int
	}{
		{"first page", 5, 0, 5},
		{"second page", 5, 5, 2},
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

func TestPaymentGetByStatus(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewPaymentRepository(testDB)
	ctx := context.Background()

	// Create payments with different statuses
	for range 3 {
		p := newTestPayment(uuid.New(), func(p *models.Payment) { p.Status = models.PaymentPending })
		require.NoError(t, testDB.Create(p).Error)
	}
	for range 2 {
		p := newTestPayment(uuid.New(), func(p *models.Payment) { p.Status = models.PaymentCompleted })
		require.NoError(t, testDB.Create(p).Error)
	}

	pending, err := repo.GetByStatus(ctx, models.PaymentPending, 10, 0)
	require.NoError(t, err)
	assert.Len(t, pending, 3)

	completed, err := repo.GetByStatus(ctx, models.PaymentCompleted, 10, 0)
	require.NoError(t, err)
	assert.Len(t, completed, 2)
}
