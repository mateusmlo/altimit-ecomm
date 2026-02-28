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

func TestInventoryCreate_Success(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewInventoryRepository(testDB)
	ctx := context.Background()

	product := newTestProduct()
	product.ID = uuid.Nil // let the repo assign an ID

	err := repo.Create(ctx, product)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, product.ID)

	fetched, err := repo.GetByID(ctx, product.ID)
	require.NoError(t, err)
	assert.Equal(t, product.Name, fetched.Name)
	assert.Equal(t, product.Stock, fetched.Stock)
}

func TestInventoryCreate_WithExistingID(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewInventoryRepository(testDB)
	ctx := context.Background()

	fixedID := uuid.New()
	product := newTestProduct(func(p *models.Product) { p.ID = fixedID })

	err := repo.Create(ctx, product)
	require.NoError(t, err)
	assert.Equal(t, fixedID, product.ID)
}

func TestInventoryGetByID_NotFound(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewInventoryRepository(testDB)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, uuid.New())

	require.Error(t, err)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestInventoryGetByIDs(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewInventoryRepository(testDB)
	ctx := context.Background()

	p1 := newTestProduct()
	p2 := newTestProduct()
	p3 := newTestProduct()
	require.NoError(t, testDB.Create(p1).Error)
	require.NoError(t, testDB.Create(p2).Error)
	require.NoError(t, testDB.Create(p3).Error)

	products, err := repo.GetByIDs(ctx, []uuid.UUID{p1.ID, p3.ID})

	require.NoError(t, err)
	assert.Len(t, products, 2)
}

func TestInventoryUpdate(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewInventoryRepository(testDB)
	ctx := context.Background()

	product := newTestProduct()
	require.NoError(t, testDB.Create(product).Error)

	product.Name = "Updated Name"
	product.Price = 49.99

	err := repo.Update(ctx, product)
	require.NoError(t, err)

	fetched, err := repo.GetByID(ctx, product.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated Name", fetched.Name)
	assert.InDelta(t, 49.99, fetched.Price, 0.01)
}

func TestInventoryDelete(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewInventoryRepository(testDB)
	ctx := context.Background()

	product := newTestProduct()
	require.NoError(t, testDB.Create(product).Error)

	err := repo.Delete(ctx, product.ID)
	require.NoError(t, err)

	_, err = repo.GetByID(ctx, product.ID)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestInventoryList_Pagination(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewInventoryRepository(testDB)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		require.NoError(t, testDB.Create(newTestProduct()).Error)
	}

	tests := []struct {
		name          string
		limit, offset int
		expectedCount int
	}{
		{"first page", 5, 0, 5},
		{"second page", 5, 5, 5},
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

func TestBatchCheckAvailability_AllAvailable(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewInventoryRepository(testDB)
	ctx := context.Background()

	p1 := newTestProduct(func(p *models.Product) { p.Stock = 100 })
	p2 := newTestProduct(func(p *models.Product) { p.Stock = 50 })
	require.NoError(t, testDB.Create(p1).Error)
	require.NoError(t, testDB.Create(p2).Error)

	items := []models.InventoryProduct{
		{ProductID: p1.ID, Quantity: 10},
		{ProductID: p2.ID, Quantity: 50},
	}

	result, err := repo.BatchCheckAvailability(ctx, items)
	require.NoError(t, err)
	assert.True(t, result[p1.ID])
	assert.True(t, result[p2.ID])
}

func TestBatchCheckAvailability_SomeUnavailable(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewInventoryRepository(testDB)
	ctx := context.Background()

	p1 := newTestProduct(func(p *models.Product) { p.Stock = 100 })
	p2 := newTestProduct(func(p *models.Product) { p.Stock = 5 })
	require.NoError(t, testDB.Create(p1).Error)
	require.NoError(t, testDB.Create(p2).Error)

	items := []models.InventoryProduct{
		{ProductID: p1.ID, Quantity: 10},
		{ProductID: p2.ID, Quantity: 20},
	}

	result, err := repo.BatchCheckAvailability(ctx, items)
	require.NoError(t, err)
	assert.True(t, result[p1.ID])
	assert.False(t, result[p2.ID])
}

func TestBatchCheckAvailability_MissingProducts(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewInventoryRepository(testDB)
	ctx := context.Background()

	missingID := uuid.New()
	items := []models.InventoryProduct{
		{ProductID: missingID, Quantity: 5},
	}

	result, err := repo.BatchCheckAvailability(ctx, items)
	require.NoError(t, err)
	assert.False(t, result[missingID])
}

func TestBatchCheckAvailability_Empty(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewInventoryRepository(testDB)
	ctx := context.Background()

	result, err := repo.BatchCheckAvailability(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, result)
}
