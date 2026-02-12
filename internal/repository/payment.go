package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"gorm.io/gorm"
)

type PaymentRepository interface {
	Create(ctx context.Context, payment *models.Payment) error
	GetByID(ctx context.Context, id uuid.UUID) (*models.Payment, error)
	GetByOrderID(ctx context.Context, orderID uuid.UUID) (*models.Payment, error)
	Update(ctx context.Context, payment *models.Payment) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status models.PaymentStatus) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, limit, offset int) ([]*models.Payment, error)
	GetByStatus(ctx context.Context, status models.PaymentStatus, limit, offset int) ([]*models.Payment, error)
}

type paymentRepository struct {
	db *gorm.DB
}

func NewPaymentRepository(db *gorm.DB) PaymentRepository {
	return &paymentRepository{db: db}
}

func (r *paymentRepository) Create(ctx context.Context, payment *models.Payment) error {
	if payment.ID == uuid.Nil {
		payment.ID = uuid.New()
	}
	payment.CreatedAt = time.Now()
	return r.db.WithContext(ctx).Create(payment).Error
}

func (r *paymentRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.Payment, error) {
	var payment models.Payment
	err := r.db.WithContext(ctx).First(&payment, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &payment, nil
}

func (r *paymentRepository) GetByOrderID(ctx context.Context, orderID uuid.UUID) (*models.Payment, error) {
	var payment models.Payment
	err := r.db.WithContext(ctx).First(&payment, "order_id = ?", orderID).Error
	if err != nil {
		return nil, err
	}
	return &payment, nil
}

func (r *paymentRepository) Update(ctx context.Context, payment *models.Payment) error {
	return r.db.WithContext(ctx).
		Model(&models.Payment{}).
		Where("id = ?", payment.ID).
		Updates(payment).Error
}

func (r *paymentRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status models.PaymentStatus) error {
	return r.db.WithContext(ctx).
		Model(&models.Payment{}).
		Where("id = ?", id).
		Update("status", status).Error
}

func (r *paymentRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.Payment{}, "id = ?", id).Error
}

func (r *paymentRepository) List(ctx context.Context, limit, offset int) ([]*models.Payment, error) {
	var payments []*models.Payment
	err := r.db.WithContext(ctx).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&payments).Error
	if err != nil {
		return nil, err
	}
	return payments, nil
}

func (r *paymentRepository) GetByStatus(ctx context.Context, status models.PaymentStatus, limit, offset int) ([]*models.Payment, error) {
	var payments []*models.Payment
	err := r.db.WithContext(ctx).
		Where("status = ?", status).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&payments).Error
	if err != nil {
		return nil, err
	}
	return payments, nil
}
