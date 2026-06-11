package errs_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/mateusmlo/altimit-ecomm/internal/errs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- InsufficientStockError ----

func TestInsufficientStockError_Is(t *testing.T) {
	err := &errs.InsufficientStockError{ProductID: uuid.New(), Requested: 10, Available: 5}
	assert.True(t, errors.Is(err, errs.ErrInsufficientStock))
}

func TestInsufficientStockError_As(t *testing.T) {
	productID := uuid.New()
	err := &errs.InsufficientStockError{ProductID: productID, Requested: 10, Available: 5}

	var target *errs.InsufficientStockError
	require.True(t, errors.As(err, &target))
	assert.Equal(t, productID, target.ProductID)
	assert.Equal(t, 10, target.Requested)
	assert.Equal(t, 5, target.Available)
}

func TestInsufficientStockError_WrappedChain(t *testing.T) {
	productID := uuid.New()
	inner := &errs.InsufficientStockError{ProductID: productID, Requested: 10, Available: 5}
	wrapped := fmt.Errorf("repository: %w", inner)

	assert.True(t, errors.Is(wrapped, errs.ErrInsufficientStock))

	var target *errs.InsufficientStockError
	require.True(t, errors.As(wrapped, &target))
	assert.Equal(t, productID, target.ProductID)
	assert.Equal(t, 10, target.Requested)
	assert.Equal(t, 5, target.Available)
}

// ---- NoActiveReservationsError ----

func TestNoActiveReservationsError_Is(t *testing.T) {
	err := &errs.NoActiveReservationsError{OrderID: uuid.New(), ProductID: uuid.New()}
	assert.True(t, errors.Is(err, errs.ErrNoActiveReservations))
}

func TestNoActiveReservationsError_As(t *testing.T) {
	orderID := uuid.New()
	productID := uuid.New()
	err := &errs.NoActiveReservationsError{OrderID: orderID, ProductID: productID}

	var target *errs.NoActiveReservationsError
	require.True(t, errors.As(err, &target))
	assert.Equal(t, orderID, target.OrderID)
	assert.Equal(t, productID, target.ProductID)
}

func TestNoActiveReservationsError_WrappedChain(t *testing.T) {
	orderID := uuid.New()
	inner := &errs.NoActiveReservationsError{OrderID: orderID, ProductID: uuid.New()}
	wrapped := fmt.Errorf("repository: %w", inner)

	assert.True(t, errors.Is(wrapped, errs.ErrNoActiveReservations))

	var target *errs.NoActiveReservationsError
	require.True(t, errors.As(wrapped, &target))
	assert.Equal(t, orderID, target.OrderID)
}

// ---- QuantityMismatchError ----

func TestQuantityMismatchError_Is(t *testing.T) {
	err := &errs.QuantityMismatchError{ProductID: uuid.New(), ReservationQuantity: 10, ItemQuantity: 5}
	assert.True(t, errors.Is(err, errs.ErrQuantityMismatch))
}

func TestQuantityMismatchError_As(t *testing.T) {
	productID := uuid.New()
	err := &errs.QuantityMismatchError{ProductID: productID, ReservationQuantity: 10, ItemQuantity: 5}

	var target *errs.QuantityMismatchError
	require.True(t, errors.As(err, &target))
	assert.Equal(t, productID, target.ProductID)
	assert.Equal(t, 10, target.ReservationQuantity)
	assert.Equal(t, 5, target.ItemQuantity)
}

func TestQuantityMismatchError_WrappedChain(t *testing.T) {
	productID := uuid.New()
	inner := &errs.QuantityMismatchError{ProductID: productID, ReservationQuantity: 10, ItemQuantity: 5}
	wrapped := fmt.Errorf("repository: %w", inner)

	assert.True(t, errors.Is(wrapped, errs.ErrQuantityMismatch))

	var target *errs.QuantityMismatchError
	require.True(t, errors.As(wrapped, &target))
	assert.Equal(t, productID, target.ProductID)
	assert.Equal(t, 10, target.ReservationQuantity)
	assert.Equal(t, 5, target.ItemQuantity)
}
