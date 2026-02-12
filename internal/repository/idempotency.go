package repository

import (
	"context"
	"time"

	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"gorm.io/gorm"
)

type IdempotencyRepository interface {
	Create(ctx context.Context, key *models.IdempotencyKey) error
	GetByKey(ctx context.Context, key string) (*models.IdempotencyKey, error)
	Exists(ctx context.Context, key string) (bool, error)
	Delete(ctx context.Context, key string) error
	DeleteOlderThan(ctx context.Context, duration time.Duration) error
}

type idempotencyRepository struct {
	db *gorm.DB
}

func NewIdempotencyRepository(db *gorm.DB) IdempotencyRepository {
	return &idempotencyRepository{db: db}
}

func (r *idempotencyRepository) Create(ctx context.Context, key *models.IdempotencyKey) error {
	key.CreatedAt = time.Now()
	return r.db.WithContext(ctx).Create(key).Error
}

func (r *idempotencyRepository) GetByKey(ctx context.Context, key string) (*models.IdempotencyKey, error) {
	var idempotencyKey models.IdempotencyKey
	err := r.db.WithContext(ctx).First(&idempotencyKey, "key = ?", key).Error
	if err != nil {
		return nil, err
	}
	return &idempotencyKey, nil
}

func (r *idempotencyRepository) Exists(ctx context.Context, key string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.IdempotencyKey{}).
		Where("key = ?", key).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *idempotencyRepository) Delete(ctx context.Context, key string) error {
	return r.db.WithContext(ctx).Delete(&models.IdempotencyKey{}, "key = ?", key).Error
}

func (r *idempotencyRepository) DeleteOlderThan(ctx context.Context, duration time.Duration) error {
	threshold := time.Now().Add(-duration)
	return r.db.WithContext(ctx).
		Where("created_at < ?", threshold).
		Delete(&models.IdempotencyKey{}).Error
}
