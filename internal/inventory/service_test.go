package inventory

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

// mockInventoryRepo is a hand-written mock for InventoryReservationRepository.
type mockInventoryRepo struct {
	reserveErr    error
	releaseErr    error
	reserveCalled bool
	releaseCalled bool
}

func (m *mockInventoryRepo) ReserveItems(_ context.Context, _ uuid.UUID, _ []models.InventoryProduct) error {
	m.reserveCalled = true
	return m.reserveErr
}

func (m *mockInventoryRepo) ReleaseItems(_ context.Context, _ uuid.UUID, _ []models.InventoryProduct) error {
	m.releaseCalled = true
	return m.releaseErr
}

func (m *mockInventoryRepo) ConfirmByOrderID(_ context.Context, _ uuid.UUID) error {
	return nil
}

func (m *mockInventoryRepo) FindByOrderID(_ context.Context, _ uuid.UUID) ([]models.InventoryReservation, error) {
	return nil, nil
}

func (m *mockInventoryRepo) FindByProductID(_ context.Context, _ uuid.UUID) ([]models.InventoryReservation, error) {
	return nil, nil
}

var (
	validProducts = []models.InventoryProduct{
		{ProductID: uuid.New(), Quantity: 5},
	}
	validOrderID = uuid.New()
)

// ---- ReserveInventory ----

func TestReserveInventory_EmptyProducts(t *testing.T) {
	svc := NewInventoryService(&mockInventoryRepo{})

	reply, err := svc.ReserveInventory(context.Background(), validOrderID, models.ReserveInventoryCommand{
		Products: []models.InventoryProduct{},
	})

	require.NoError(t, err)
	assert.False(t, reply.Success)
	assert.Equal(t, "No products specified", reply.Message)
}

func TestReserveInventory_InvalidQuantity(t *testing.T) {
	svc := NewInventoryService(&mockInventoryRepo{})
	productID := uuid.New()

	reply, err := svc.ReserveInventory(context.Background(), validOrderID, models.ReserveInventoryCommand{
		Products: []models.InventoryProduct{
			{ProductID: productID, Quantity: 0},
		},
	})

	require.NoError(t, err)
	assert.False(t, reply.Success)
	assert.Contains(t, reply.Message, "Invalid quantity")
}

func TestReserveInventory_Success(t *testing.T) {
	svc := NewInventoryService(&mockInventoryRepo{reserveErr: nil})

	reply, err := svc.ReserveInventory(context.Background(), validOrderID, models.ReserveInventoryCommand{
		Products: validProducts,
	})

	require.NoError(t, err)
	assert.True(t, reply.Success)
	assert.Equal(t, "Inventory reserved successfully", reply.Message)
}

func TestReserveInventory_InsufficientStock(t *testing.T) {
	productID := uuid.New()
	svc := NewInventoryService(&mockInventoryRepo{
		reserveErr: &errs.InsufficientStockError{
			ProductID: productID,
			Requested: 10,
			Available: 5,
		},
	})

	reply, err := svc.ReserveInventory(context.Background(), validOrderID, models.ReserveInventoryCommand{
		Products: validProducts,
	})

	require.NoError(t, err)
	assert.False(t, reply.Success)
	assert.Equal(t, "Insufficient stock", reply.Message)
}

func TestReserveInventory_ProductNotFound(t *testing.T) {
	svc := NewInventoryService(&mockInventoryRepo{
		reserveErr: errs.ErrProductNotFound,
	})

	reply, err := svc.ReserveInventory(context.Background(), validOrderID, models.ReserveInventoryCommand{
		Products: validProducts,
	})

	require.NoError(t, err)
	assert.False(t, reply.Success)
	assert.Equal(t, "Product not found", reply.Message)
}

func TestReserveInventory_UnexpectedError(t *testing.T) {
	unexpected := errors.New("db connection lost")
	svc := NewInventoryService(&mockInventoryRepo{reserveErr: unexpected})

	reply, err := svc.ReserveInventory(context.Background(), validOrderID, models.ReserveInventoryCommand{
		Products: validProducts,
	})

	assert.Nil(t, reply)
	assert.ErrorIs(t, err, unexpected)
}

// ---- ReleaseInventory ----

func TestReleaseInventory_Success(t *testing.T) {
	svc := NewInventoryService(&mockInventoryRepo{releaseErr: nil})

	reply, err := svc.ReleaseInventory(context.Background(), validOrderID, models.ReleaseInventoryCommand{
		Products: validProducts,
	})

	require.NoError(t, err)
	assert.True(t, reply.Success)
	assert.Equal(t, "Inventory released successfully", reply.Message)
}

func TestReleaseInventory_ProductNotFound(t *testing.T) {
	svc := NewInventoryService(&mockInventoryRepo{
		releaseErr: errs.ErrProductNotFound,
	})

	reply, err := svc.ReleaseInventory(context.Background(), validOrderID, models.ReleaseInventoryCommand{
		Products: validProducts,
	})

	require.NoError(t, err)
	assert.False(t, reply.Success)
	assert.Equal(t, "Product not found", reply.Message)
}

func TestReleaseInventory_NoActiveReservations(t *testing.T) {
	svc := NewInventoryService(&mockInventoryRepo{
		releaseErr: &errs.NoActiveReservationsError{
			OrderID:   validOrderID,
			ProductID: uuid.New(),
		},
	})

	reply, err := svc.ReleaseInventory(context.Background(), validOrderID, models.ReleaseInventoryCommand{
		Products: validProducts,
	})

	require.NoError(t, err)
	assert.False(t, reply.Success)
	assert.Equal(t, "No active reservations", reply.Message)
}

func TestReleaseInventory_QuantityMismatch(t *testing.T) {
	productID := uuid.New()
	svc := NewInventoryService(&mockInventoryRepo{
		releaseErr: &errs.QuantityMismatchError{
			ProductID:           productID,
			ReservationQuantity: 10,
			ItemQuantity:        5,
		},
	})

	reply, err := svc.ReleaseInventory(context.Background(), validOrderID, models.ReleaseInventoryCommand{
		Products: validProducts,
	})

	require.NoError(t, err)
	assert.False(t, reply.Success)
	assert.Equal(t, "Quantity mismatch", reply.Message)
}

func TestReleaseInventory_UnexpectedError(t *testing.T) {
	unexpected := errors.New("timeout")
	svc := NewInventoryService(&mockInventoryRepo{releaseErr: unexpected})

	reply, err := svc.ReleaseInventory(context.Background(), validOrderID, models.ReleaseInventoryCommand{
		Products: validProducts,
	})

	assert.Nil(t, reply)
	assert.ErrorIs(t, err, unexpected)
}
