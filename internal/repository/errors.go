package repository

import "errors"

var (
	ErrProductNotFound      = errors.New("product not found")
	ErrInsufficientStock    = errors.New("insufficient stock")
	ErrNoActiveReservations = errors.New("no active reservations found")
)
