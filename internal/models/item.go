package models

import "github.com/google/uuid"

type Product struct {
	ID          uuid.UUID `json:"id" gorm:"type:uuid;primaryKey;column:product_id"`
	Name        string    `json:"name" gorm:"type:varchar(255);not null"`
	Description string    `json:"description,omitempty" gorm:"type:text"`
	Price       float64   `json:"price" gorm:"type:decimal(10,2);not null;check:price >= 0"`
	Stock       int       `json:"stock" gorm:"not null;check:stock >= 0"`
}
