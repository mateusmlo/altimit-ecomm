package models

import (
	"time"

	"github.com/google/uuid"
)

type Order struct {
	ID         uuid.UUID   `json:"id"`
	PublicID   string      `json:"public_id"`
	CustomerID string      `json:"customer_id"`
	Items      []OrderItem `json:"items"`
	Status     OrderStatus `json:"status"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

type OrderItem struct {
	ID       uuid.UUID `json:"id"`
	OrderID  uuid.UUID `json:"order_id"`
	ItemID   uuid.UUID `json:"item_id"`
	Quantity int       `json:"quantity"`
	Price    float64   `json:"price"`
}

type Item struct {
	ID          uuid.UUID `json:"id"`
	Price       float64   `json:"price"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Stock       int
}

func (o *Order) Total() float64 {
	total := 0.0

	for _, orderItem := range o.Items {
		total += orderItem.Price * float64(orderItem.Quantity)
	}

	return total
}
