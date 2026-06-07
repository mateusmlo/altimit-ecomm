package errs

import "errors"

// Domain errors shared across internal packages.
// Use errors.Is() to check for these in callers.
var (
	// Inventory / stock
	ErrProductNotFound      = errors.New("product not found")
	ErrInsufficientStock    = errors.New("insufficient stock")
	ErrNoActiveReservations = errors.New("no active reservations found")
	ErrQuantityMismatch     = errors.New("quantity mismatch")

	// Kafka record handling
	ErrMissingMetadata  = errors.New("metadata header not found")
	ErrMalformedPayload = errors.New("malformed payload")
	ErrUnknownEvent     = errors.New("unknown event type")

	// Orders
	ErrOrderInProgress = errors.New("order already in progress")
	ErrOrderNotFound   = errors.New("order not found")

	// Saga
	ErrCompensationFailed = errors.New("compensation permanently failed")
	ErrSagaNotFound       = errors.New("saga not found")

	// Payment
	ErrPaymentNotFound = errors.New("payment not found")
)
