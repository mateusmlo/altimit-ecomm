package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// OrderStatus represents the status of an order.
type OrderStatus string

const (
	OrderPending    OrderStatus = "PENDING"
	OrderProcessing OrderStatus = "PROCESSING"
	OrderCompleted  OrderStatus = "COMPLETED"
	OrderCancelled  OrderStatus = "CANCELLED"
	OrderFailed     OrderStatus = "FAILED"
)

type Order struct {
	ID          uuid.UUID   `json:"id" gorm:"type:uuid;primaryKey"`
	PublicID    string      `json:"public_id" gorm:"type:varchar(255);not null;index"`
	CustomerID  string      `json:"customer_id" gorm:"type:varchar(255);not null;index"`
	TotalAmount float64     `json:"total_amount" gorm:"type:decimal(10,2);not null"`
	Currency    string      `json:"currency" gorm:"type:varchar(3);not null;default:'USD'"`
	Items       []OrderItem `json:"items" gorm:"foreignKey:OrderID"`
	Status      OrderStatus `json:"status" gorm:"type:order_status;not null;index"`
	CreatedAt   time.Time   `json:"created_at" gorm:"not null;default:CURRENT_TIMESTAMP;index"`
	UpdatedAt   time.Time   `json:"updated_at" gorm:"not null;default:CURRENT_TIMESTAMP"`
}

func (o *Order) BeforeCreate(tx *gorm.DB) error {
	o.ID = uuid.New()
	return nil
}

func (o *Order) Total() float64 {
	total := 0.0

	for _, orderItem := range o.Items {
		total += orderItem.Price * float64(orderItem.Quantity)
	}

	return total
}
