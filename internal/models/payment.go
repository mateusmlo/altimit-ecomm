package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type PaymentStatus string

const (
	PaymentPending    PaymentStatus = "PENDING"
	PaymentProcessing PaymentStatus = "PROCESSING"
	PaymentCompleted  PaymentStatus = "COMPLETED"
	PaymentFailed     PaymentStatus = "FAILED"
	PaymentRefunded   PaymentStatus = "REFUNDED"
)

func (p *Payment) BeforeCreate(tx *gorm.DB) error {
	p.ID = uuid.New()
	return nil
}

type Payment struct {
	ID        uuid.UUID     `json:"id" gorm:"type:uuid;primaryKey"`
	OrderID   uuid.UUID     `json:"order_id" gorm:"type:uuid;not null"`
	Amount    float64       `json:"amount" gorm:"type:decimal(10,2);not null"`
	Status    PaymentStatus `json:"status" gorm:"type:payment_status;not null;default:PENDING"`
	CreatedAt time.Time     `json:"created_at" gorm:"not null;default:CURRENT_TIMESTAMP"`
}
