package errs

import (
	"fmt"

	"github.com/google/uuid"
)

type InsufficientStockError struct {
	ProductID uuid.UUID
	Requested int
	Available int
}

func (e *InsufficientStockError) Error() string {
	return fmt.Sprintf("insufficient stock for product %s: requested %d, available %d",
		e.ProductID, e.Requested, e.Available)
}

func (e *InsufficientStockError) Unwrap() error {
	return ErrInsufficientStock
}
