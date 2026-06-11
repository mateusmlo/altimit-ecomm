// Package errs defines domain errors shared across internal packages.
//
// # Convention
//
// Use a plain sentinel (errors.New) when the error is a bare signal — the
// category is all a caller needs:
//
//	if errors.Is(err, errs.ErrOrderNotFound) { ... }
//
// Use a typed error (a struct implementing error) when the caller may need to
// read specific fields programmatically — for example, to emit structured log
// fields or build a detailed reply. Every typed error wraps its corresponding
// sentinel via Unwrap, so errors.Is still works alongside errors.As:
//
//	var stockErr *errs.InsufficientStockError
//	if errors.As(err, &stockErr) {
//	    // stockErr.ProductID, .Requested, .Available
//	}
//
// # Sentinels
//
// These are bare signals; callers match on category only:
//
//   - ErrProductNotFound, ErrOrderNotFound, ErrSagaNotFound, ErrPaymentNotFound
//   - ErrOrderInProgress
//   - ErrUnknownEvent, ErrMissingMetadata, ErrMalformedPayload
//   - ErrCompensationFailed
//
// # Typed errors
//
// These carry fields a caller reads programmatically. Each wraps its sentinel
// via Unwrap so errors.Is on the sentinel still matches:
//
//   - [InsufficientStockError]    (sentinel: ErrInsufficientStock)    — ProductID, Requested, Available
//   - [NoActiveReservationsError] (sentinel: ErrNoActiveReservations) — OrderID, ProductID
//   - [QuantityMismatchError]     (sentinel: ErrQuantityMismatch)     — ProductID, ReservationQuantity, ItemQuantity
//
// # Adding a new error
//
// Ask: does any caller need to inspect a field on this error beyond its
// category? If yes, add a typed error with an Unwrap method returning the
// sentinel. If no, a sentinel is sufficient.
package errs
