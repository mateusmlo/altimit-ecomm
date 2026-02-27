package models

import (
	"time"

	"github.com/google/uuid"
)

type ReservationStatus string

const (
	ReservationActive    ReservationStatus = "ACTIVE"
	ReservationReleased  ReservationStatus = "RELEASED"
	ReservationConfirmed ReservationStatus = "CONFIRMED"
)

type InventoryReservation struct {
	ID        uuid.UUID         `json:"id"         gorm:"type:uuid;primaryKey"`
	// TODO: add gorm:"constraint:..." FK to Order once orders service shares this DB
	OrderID   uuid.UUID         `json:"order_id"   gorm:"type:uuid;not null;index"`
	ProductID uuid.UUID         `json:"item_id"    gorm:"type:uuid;column:product_id;not null;index"`
	Quantity  int               `json:"quantity"    gorm:"not null"`
	Status    ReservationStatus `json:"status"      gorm:"type:varchar(20);not null;default:'ACTIVE'"`
	CreatedAt time.Time         `json:"created_at"  gorm:"not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt time.Time         `json:"updated_at"  gorm:"not null;default:CURRENT_TIMESTAMP"`
}
