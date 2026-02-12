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

func TestOrderCreate_WithItems(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewOrderRepository(testDB)
	ctx := context.Background()

	order := newTestOrder(func(o *models.Order) {
		o.Items = []models.OrderItem{
			{ItemID: uuid.New(), Quantity: 2, Price: 10.00},
			{ItemID: uuid.New(), Quantity: 1, Price: 25.00},
		}
	})

	err := repo.Create(ctx, order)
	require.NoError(t, err)

	// Total should be calculated: (2*10) + (1*25) = 45
	assert.InDelta(t, 45.00, order.TotalAmount, 0.01)

	fetched, err := repo.GetByID(ctx, order.ID)
	require.NoError(t, err)
	assert.Len(t, fetched.Items, 2)
	assert.InDelta(t, 45.00, fetched.TotalAmount, 0.01)
}

func TestOrderCreate_NoItems(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewOrderRepository(testDB)
	ctx := context.Background()

	order := newTestOrder()

	err := repo.Create(ctx, order)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, order.ID)
}

func TestOrderGetByID_NotFound(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewOrderRepository(testDB)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, uuid.New())

	require.Error(t, err)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestOrderGetByPublicID(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewOrderRepository(testDB)
	ctx := context.Background()

	order := newTestOrder()
	require.NoError(t, repo.Create(ctx, order))

	fetched, err := repo.GetByPublicID(ctx, order.PublicID)
	require.NoError(t, err)
	assert.Equal(t, order.ID, fetched.ID)
}

func TestOrderGetByCustomerID_Pagination(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewOrderRepository(testDB)
	ctx := context.Background()

	customerID := "customer-test"
	for i := 0; i < 5; i++ {
		o := newTestOrder(func(o *models.Order) { o.CustomerID = customerID })
		require.NoError(t, repo.Create(ctx, o))
	}
	// Another customer's order
	other := newTestOrder(func(o *models.Order) { o.CustomerID = "other-customer" })
	require.NoError(t, repo.Create(ctx, other))

	orders, err := repo.GetByCustomerID(ctx, customerID, 3, 0)
	require.NoError(t, err)
	assert.Len(t, orders, 3)

	orders, err = repo.GetByCustomerID(ctx, customerID, 3, 3)
	require.NoError(t, err)
	assert.Len(t, orders, 2)
}

func TestOrderUpdateStatus(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewOrderRepository(testDB)
	ctx := context.Background()

	order := newTestOrder()
	require.NoError(t, repo.Create(ctx, order))

	err := repo.UpdateStatus(ctx, order.ID, models.OrderProcessing)
	require.NoError(t, err)

	fetched, err := repo.GetByID(ctx, order.ID)
	require.NoError(t, err)
	assert.Equal(t, models.OrderProcessing, fetched.Status)
}

func TestOrderDelete(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewOrderRepository(testDB)
	ctx := context.Background()

	order := newTestOrder()
	require.NoError(t, repo.Create(ctx, order))

	err := repo.Delete(ctx, order.ID)
	require.NoError(t, err)

	_, err = repo.GetByID(ctx, order.ID)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestOrderList_Pagination(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewOrderRepository(testDB)
	ctx := context.Background()

	for i := 0; i < 8; i++ {
		require.NoError(t, repo.Create(ctx, newTestOrder()))
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
