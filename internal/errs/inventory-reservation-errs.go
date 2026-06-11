package errs

import (
	"fmt"

	"github.com/google/uuid"
)

type NoActiveReservationsError struct {
	OrderID   uuid.UUID
	ProductID uuid.UUID
}

type QuantityMismatchError struct {
	ProductID           uuid.UUID
	ReservationQuantity int
	ItemQuantity        int
}

func (e *NoActiveReservationsError) Error() string {
	return fmt.Sprintf("no active reservations for product %s in order %s",
		e.ProductID, e.OrderID)
}

func (e *NoActiveReservationsError) Unwrap() error {
	return ErrNoActiveReservations
}

func (e *QuantityMismatchError) Error() string {
	return fmt.Sprintf("quantity mismatch for product %s: reserved %d, requested %d",
		e.ProductID, e.ReservationQuantity, e.ItemQuantity)
}

func (e *QuantityMismatchError) Unwrap() error {
	return ErrQuantityMismatch
}
