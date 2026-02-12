package models

import (
	"time"

	"github.com/bytedance/sonic"
)

type IdempotencyKey struct {
	Key       string                 `json:"key" gorm:"type:varchar(255);primaryKey"`
	Response  sonic.NoCopyRawMessage `json:"response" gorm:"type:jsonb;not null"`
	CreatedAt time.Time              `json:"created_at" gorm:"not null;default:CURRENT_TIMESTAMP"`
}
