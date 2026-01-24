package models

import (
	"time"

	"github.com/google/uuid"
)

type Order struct {
	ID         uuid.UUID      `json:"id"`
	PublicID   string         `json:"public_id"`
	CustomerID string         `json:"customer_id"`
	Products   []OrderProduct `json:"products"`
	Status     OrderStatus    `json:"status"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

type OrderProduct struct {
	ID        uuid.UUID `json:"id"`
	OrderID   uuid.UUID `json:"order_id"`
	ProductID uuid.UUID `json:"product_id"`
	Quantity  int       `json:"quantity"`
	Price     float64   `json:"price"`
}

type Product struct {
	ID          uuid.UUID `json:"id"`
	Price       float64   `json:"price"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Stock       int
}

func (o *Order) Total() float64 {
	total := 0.0

	for _, orderProd := range o.Products {
		total += orderProd.Price * float64(orderProd.Quantity)
	}

	return total
}
