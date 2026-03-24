package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"gorm.io/gorm"
)

type OrderRepository interface {
	Create(ctx context.Context, order *models.Order) error
	GetByID(ctx context.Context, id uuid.UUID) (*models.Order, error)
	GetByPublicID(ctx context.Context, publicID string) (*models.Order, error)
	GetByCustomerID(ctx context.Context, customerID string, limit, offset int) ([]*models.Order, error)
	Update(ctx context.Context, order *models.Order) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status models.OrderStatus) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, limit, offset int) ([]*models.Order, error)
}

type orderRepository struct {
	db *gorm.DB
}

func NewOrderRepository(db *gorm.DB) OrderRepository {
	return &orderRepository{db: db}
}

func (r *orderRepository) Create(ctx context.Context, order *models.Order) error {
	order.CreatedAt = time.Now()
	order.UpdatedAt = time.Now()
	order.TotalAmount = order.Total()

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Create(order).Error
	})
}

func (r *orderRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.Order, error) {
	var order models.Order

	err := r.db.WithContext(ctx).
		Preload("Items").
		First(&order, "id = ?", id).Error
	if err != nil {
		return nil, err
	}

	return &order, nil
}

func (r *orderRepository) GetByPublicID(ctx context.Context, publicID string) (*models.Order, error) {
	var order models.Order

	err := r.db.WithContext(ctx).
		Preload("Items").
		First(&order, "public_id = ?", publicID).Error
	if err != nil {
		return nil, err
	}

	return &order, nil
}

func (r *orderRepository) GetByCustomerID(ctx context.Context, customerID string, limit, offset int) ([]*models.Order, error) {
	var orders []*models.Order

	err := r.db.WithContext(ctx).
		Preload("Items").
		Where("customer_id = ?", customerID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&orders).Error
	if err != nil {
		return nil, err
	}

	return orders, nil
}

func (r *orderRepository) Update(ctx context.Context, order *models.Order) error {
	order.UpdatedAt = time.Now()

	return r.db.WithContext(ctx).
		Model(&models.Order{}).
		Where("id = ?", order.ID).
		Updates(order).Error
}

func (r *orderRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status models.OrderStatus) error {
	return r.db.WithContext(ctx).
		Model(&models.Order{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":     status,
			"updated_at": time.Now(),
		}).Error
}

func (r *orderRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.Order{}, "id = ?", id).Error
}

func (r *orderRepository) List(ctx context.Context, limit, offset int) ([]*models.Order, error) {
	var orders []*models.Order

	err := r.db.WithContext(ctx).
		Preload("Items").
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&orders).Error
	if err != nil {
		return nil, err
	}

	return orders, nil
}
