package errs

import "errors"

// Domain errors shared across internal packages.
// Use errors.Is() to check for these in callers.
var (
	// Inventory / stock
	ErrProductNotFound      = errors.New("product not found")
	ErrInsufficientStock    = errors.New("insufficient stock")
	ErrNoActiveReservations = errors.New("no active reservations found")

	// Kafka record handling
	ErrMissingMetadata  = errors.New("metadata header not found")
	ErrMalformedPayload = errors.New("malformed payload")
	ErrUnknownEvent     = errors.New("unknown event type")

	// Orders
	ErrOrderInProgress = errors.New("order already in progress")
)
