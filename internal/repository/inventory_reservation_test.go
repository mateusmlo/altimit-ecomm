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
)

func TestReserveItems_Success(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewInventoryReservationRepository(testDB)
	ctx := context.Background()

	p1 := newTestProduct(func(p *models.Product) { p.Stock = 100 })
	p2 := newTestProduct(func(p *models.Product) { p.Stock = 50 })
	require.NoError(t, testDB.Create(p1).Error)
	require.NoError(t, testDB.Create(p2).Error)

	orderID := uuid.New()
	items := []models.InventoryProduct{
		{ProductID: p1.ID, Quantity: 10},
		{ProductID: p2.ID, Quantity: 20},
	}

	err := repo.ReserveItems(ctx, orderID, items)
	require.NoError(t, err)

	// Verify stock decremented
	var actual1, actual2 models.Product
	require.NoError(t, testDB.First(&actual1, "product_id = ?", p1.ID).Error)
	require.NoError(t, testDB.First(&actual2, "product_id = ?", p2.ID).Error)
	assert.Equal(t, 90, actual1.Stock)
	assert.Equal(t, 30, actual2.Stock)

	// Verify reservations created
	reservations, err := repo.FindByOrderID(ctx, orderID)
	require.NoError(t, err)
	assert.Len(t, reservations, 2)
	for _, r := range reservations {
		assert.Equal(t, models.ReservationActive, r.Status)
		assert.Equal(t, orderID, r.OrderID)
	}
}

func TestReserveItems_EmptyItems(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewInventoryReservationRepository(testDB)
	ctx := context.Background()

	err := repo.ReserveItems(ctx, uuid.New(), nil)
	assert.NoError(t, err)
}

func TestReserveItems_InsufficientStock(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewInventoryReservationRepository(testDB)
	ctx := context.Background()

	product := newTestProduct(func(p *models.Product) { p.Stock = 5 })
	require.NoError(t, testDB.Create(product).Error)

	orderID := uuid.New()
	items := []models.InventoryProduct{
		{ProductID: product.ID, Quantity: 10},
	}

	err := repo.ReserveItems(ctx, orderID, items)

	require.Error(t, err)
	assert.True(t, errors.Is(err, errs.ErrInsufficientStock))

	// Verify stock unchanged (transaction rolled back)
	var actual models.Product
	require.NoError(t, testDB.First(&actual, "product_id = ?", product.ID).Error)
	assert.Equal(t, 5, actual.Stock)

	// Verify no reservations created
	reservations, err := repo.FindByOrderID(ctx, orderID)
	require.NoError(t, err)
	assert.Empty(t, reservations)
}

func TestReserveItems_ProductNotFound(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewInventoryReservationRepository(testDB)
	ctx := context.Background()

	items := []models.InventoryProduct{
		{ProductID: uuid.New(), Quantity: 5},
	}

	err := repo.ReserveItems(ctx, uuid.New(), items)

	require.Error(t, err)
	assert.True(t, errors.Is(err, errs.ErrProductNotFound))
}

func TestReleaseItems_Success(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewInventoryReservationRepository(testDB)
	ctx := context.Background()

	product := newTestProduct(func(p *models.Product) { p.Stock = 100 })
	require.NoError(t, testDB.Create(product).Error)

	orderID := uuid.New()
	items := []models.InventoryProduct{
		{ProductID: product.ID, Quantity: 10},
	}

	// Reserve first
	require.NoError(t, repo.ReserveItems(ctx, orderID, items))

	// Then release
	err := repo.ReleaseItems(ctx, orderID, items)
	require.NoError(t, err)

	// Verify stock restored
	var actual models.Product
	require.NoError(t, testDB.First(&actual, "product_id = ?", product.ID).Error)
	assert.Equal(t, 100, actual.Stock)

	// Verify reservations marked as released
	reservations, err := repo.FindByOrderID(ctx, orderID)
	require.NoError(t, err)
	for _, r := range reservations {
		assert.Equal(t, models.ReservationReleased, r.Status)
	}
}

func TestReleaseItems_NoActiveReservations(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewInventoryReservationRepository(testDB)
	ctx := context.Background()

	// Release on an order with no reservations — should be a noop
	err := repo.ReleaseItems(ctx, uuid.New(), nil)
	assert.NoError(t, err)
}

func TestReleaseItems_QuantityMismatch(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewInventoryReservationRepository(testDB)
	ctx := context.Background()

	product := newTestProduct(func(p *models.Product) { p.Stock = 100 })
	require.NoError(t, testDB.Create(product).Error)

	orderID := uuid.New()
	require.NoError(t, repo.ReserveItems(ctx, orderID, []models.InventoryProduct{
		{ProductID: product.ID, Quantity: 10},
	}))

	// Release with a quantity that doesn't match the reservation
	err := repo.ReleaseItems(ctx, orderID, []models.InventoryProduct{
		{ProductID: product.ID, Quantity: 5},
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, errs.ErrQuantityMismatch))
}

func TestReleaseItems_AlreadyReleased(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewInventoryReservationRepository(testDB)
	ctx := context.Background()

	product := newTestProduct(func(p *models.Product) { p.Stock = 100 })
	require.NoError(t, testDB.Create(product).Error)

	orderID := uuid.New()
	items := []models.InventoryProduct{
		{ProductID: product.ID, Quantity: 10},
	}

	require.NoError(t, repo.ReserveItems(ctx, orderID, items))
	require.NoError(t, repo.ReleaseItems(ctx, orderID, items))

	// Second release — no active reservations, should return error
	err := repo.ReleaseItems(ctx, orderID, items)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errs.ErrNoActiveReservations))

	// Stock should still be 100 (not 110)
	var actual models.Product
	require.NoError(t, testDB.First(&actual, "product_id = ?", product.ID).Error)
	assert.Equal(t, 100, actual.Stock)
}

func TestConfirmByOrderID_Success(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewInventoryReservationRepository(testDB)
	ctx := context.Background()

	product := newTestProduct(func(p *models.Product) { p.Stock = 100 })
	require.NoError(t, testDB.Create(product).Error)

	orderID := uuid.New()
	items := []models.InventoryProduct{
		{ProductID: product.ID, Quantity: 10},
	}

	require.NoError(t, repo.ReserveItems(ctx, orderID, items))

	err := repo.ConfirmByOrderID(ctx, orderID)
	require.NoError(t, err)

	// Verify reservations confirmed, stock unchanged (still decremented)
	reservations, err := repo.FindByOrderID(ctx, orderID)
	require.NoError(t, err)
	for _, r := range reservations {
		assert.Equal(t, models.ReservationConfirmed, r.Status)
	}

	var actual models.Product
	require.NoError(t, testDB.First(&actual, "product_id = ?", product.ID).Error)
	assert.Equal(t, 90, actual.Stock)
}

func TestConfirmByOrderID_NoActiveReservations(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewInventoryReservationRepository(testDB)
	ctx := context.Background()

	err := repo.ConfirmByOrderID(ctx, uuid.New())

	require.Error(t, err)
	assert.True(t, errors.Is(err, errs.ErrNoActiveReservations))
}

func TestConfirmByOrderID_AlreadyConfirmed(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewInventoryReservationRepository(testDB)
	ctx := context.Background()

	product := newTestProduct(func(p *models.Product) { p.Stock = 100 })
	require.NoError(t, testDB.Create(product).Error)

	orderID := uuid.New()
	items := []models.InventoryProduct{
		{ProductID: product.ID, Quantity: 10},
	}

	require.NoError(t, repo.ReserveItems(ctx, orderID, items))
	require.NoError(t, repo.ConfirmByOrderID(ctx, orderID))

	// Second confirm — no active reservations left
	err := repo.ConfirmByOrderID(ctx, orderID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errs.ErrNoActiveReservations))
}

func TestFindByOrderID_Found(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewInventoryReservationRepository(testDB)
	ctx := context.Background()

	product := newTestProduct(func(p *models.Product) { p.Stock = 100 })
	require.NoError(t, testDB.Create(product).Error)

	orderID := uuid.New()
	items := []models.InventoryProduct{
		{ProductID: product.ID, Quantity: 5},
	}
	require.NoError(t, repo.ReserveItems(ctx, orderID, items))

	reservations, err := repo.FindByOrderID(ctx, orderID)

	require.NoError(t, err)
	require.Len(t, reservations, 1)
	assert.Equal(t, orderID, reservations[0].OrderID)
	assert.Equal(t, product.ID, reservations[0].ProductID)
	assert.Equal(t, 5, reservations[0].Quantity)
}

func TestFindByOrderID_Empty(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewInventoryReservationRepository(testDB)
	ctx := context.Background()

	reservations, err := repo.FindByOrderID(ctx, uuid.New())

	require.NoError(t, err)
	assert.Empty(t, reservations)
}

func TestFindByProductID_MultipleOrders(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewInventoryReservationRepository(testDB)
	ctx := context.Background()

	product := newTestProduct(func(p *models.Product) { p.Stock = 100 })
	require.NoError(t, testDB.Create(product).Error)

	items := []models.InventoryProduct{
		{ProductID: product.ID, Quantity: 5},
	}

	require.NoError(t, repo.ReserveItems(ctx, uuid.New(), items))
	require.NoError(t, repo.ReserveItems(ctx, uuid.New(), items))

	reservations, err := repo.FindByProductID(ctx, product.ID)

	require.NoError(t, err)
	assert.Len(t, reservations, 2)
}

func TestFilterActiveReservations(t *testing.T) {
	reservations := []models.InventoryReservation{
		{Status: models.ReservationActive},
		{Status: models.ReservationReleased},
		{Status: models.ReservationActive},
		{Status: models.ReservationConfirmed},
	}

	result := filterActiveReservations(reservations)

	assert.Len(t, result, 2)
	for _, r := range result {
		assert.Equal(t, models.ReservationActive, r.Status)
	}
}

func TestFilterActiveReservations_Empty(t *testing.T) {
	result := filterActiveReservations(nil)
	assert.Empty(t, result)
}
