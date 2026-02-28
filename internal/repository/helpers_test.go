//go:build integration

package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/mateusmlo/altimit-ecomm/internal/errs"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestExtractItemIDs_Empty(t *testing.T) {
	ids := extractItemIDs(nil)
	assert.Empty(t, ids)
}

func TestExtractItemIDs_Single(t *testing.T) {
	id := uuid.New()
	items := []models.InventoryProduct{{ProductID: id, Quantity: 5}}

	ids := extractItemIDs(items)

	require.Len(t, ids, 1)
	assert.Equal(t, id, ids[0])
}

func TestExtractItemIDs_Multiple_PreservesOrder(t *testing.T) {
	id1, id2, id3 := uuid.New(), uuid.New(), uuid.New()
	items := []models.InventoryProduct{
		{ProductID: id1, Quantity: 1},
		{ProductID: id2, Quantity: 2},
		{ProductID: id3, Quantity: 3},
	}

	ids := extractItemIDs(items)

	require.Len(t, ids, 3)
	assert.Equal(t, id1, ids[0])
	assert.Equal(t, id2, ids[1])
	assert.Equal(t, id3, ids[2])
}

func TestLockAndFetchProducts_AllFound(t *testing.T) {
	cleanupTables(t, testDB)

	p1 := newTestProduct()
	p2 := newTestProduct()
	require.NoError(t, testDB.Create(p1).Error)
	require.NoError(t, testDB.Create(p2).Error)

	tx := testDB.Begin()
	defer tx.Rollback()

	products, err := lockAndFetchProducts(tx, []uuid.UUID{p1.ID, p2.ID})

	require.NoError(t, err)
	assert.Len(t, products, 2)
}

func TestLockAndFetchProducts_PartialMissing(t *testing.T) {
	cleanupTables(t, testDB)

	p1 := newTestProduct()
	require.NoError(t, testDB.Create(p1).Error)

	missingID := uuid.New()

	tx := testDB.Begin()
	defer tx.Rollback()

	products, err := lockAndFetchProducts(tx, []uuid.UUID{p1.ID, missingID})

	require.Error(t, err)
	assert.Nil(t, products)
	assert.True(t, errors.Is(err, errs.ErrProductNotFound))
}

func TestLockAndFetchProducts_OrderedByProductID(t *testing.T) {
	cleanupTables(t, testDB)

	products := make([]*models.Product, 5)
	for i := range products {
		products[i] = newTestProduct()
		require.NoError(t, testDB.Create(products[i]).Error)
	}

	ids := make([]uuid.UUID, len(products))
	for i, p := range products {
		ids[i] = p.ID
	}

	tx := testDB.Begin()
	defer tx.Rollback()

	fetched, err := lockAndFetchProducts(tx, ids)
	require.NoError(t, err)
	require.Len(t, fetched, 5)

	for i := 1; i < len(fetched); i++ {
		assert.True(t, fetched[i-1].ID.String() <= fetched[i].ID.String(),
			"products should be ordered by product_id")
	}
}

func TestBatchUpdateProductStock_Decrement(t *testing.T) {
	cleanupTables(t, testDB)
	ctx := context.Background()

	p1 := newTestProduct(func(p *models.Product) { p.Stock = 100 })
	p2 := newTestProduct(func(p *models.Product) { p.Stock = 50 })
	require.NoError(t, testDB.Create(p1).Error)
	require.NoError(t, testDB.Create(p2).Error)

	items := []models.InventoryProduct{
		{ProductID: p1.ID, Quantity: 10},
		{ProductID: p2.ID, Quantity: 20},
	}

	err := testDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return batchUpdateProductStock(tx, MINUS_OP, items)
	})
	require.NoError(t, err)

	var actual1, actual2 models.Product
	require.NoError(t, testDB.First(&actual1, "product_id = ?", p1.ID).Error)
	require.NoError(t, testDB.First(&actual2, "product_id = ?", p2.ID).Error)

	assert.Equal(t, 90, actual1.Stock)
	assert.Equal(t, 30, actual2.Stock)
}

func TestBatchUpdateProductStock_Increment(t *testing.T) {
	cleanupTables(t, testDB)
	ctx := context.Background()

	p1 := newTestProduct(func(p *models.Product) { p.Stock = 10 })
	p2 := newTestProduct(func(p *models.Product) { p.Stock = 20 })
	require.NoError(t, testDB.Create(p1).Error)
	require.NoError(t, testDB.Create(p2).Error)

	items := []models.InventoryProduct{
		{ProductID: p1.ID, Quantity: 5},
		{ProductID: p2.ID, Quantity: 10},
	}

	err := testDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return batchUpdateProductStock(tx, PLUS_OP, items)
	})
	require.NoError(t, err)

	var actual1, actual2 models.Product
	require.NoError(t, testDB.First(&actual1, "product_id = ?", p1.ID).Error)
	require.NoError(t, testDB.First(&actual2, "product_id = ?", p2.ID).Error)

	assert.Equal(t, 15, actual1.Stock)
	assert.Equal(t, 30, actual2.Stock)
}
