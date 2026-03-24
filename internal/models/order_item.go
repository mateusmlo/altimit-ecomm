package models

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func (o *OrderItem) BeforeCreate(tx *gorm.DB) error {
	o.ID = uuid.New()
	return nil
}

type OrderItem struct {
	ID       uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	OrderID  uuid.UUID `json:"order_id" gorm:"type:uuid;not null;index"`
	ItemID   uuid.UUID `json:"item_id" gorm:"type:uuid;not null;index"`
	Quantity int       `json:"quantity" gorm:"not null"`
	Price    float64   `json:"price" gorm:"type:decimal(10,2);not null"`
}
