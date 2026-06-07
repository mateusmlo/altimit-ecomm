package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/mateusmlo/altimit-ecomm/internal/errs"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"gorm.io/gorm"
)

type SagaRepository interface {
	Create(ctx context.Context, sagaState *models.SagaState) error
	GetByID(ctx context.Context, sagaID uuid.UUID) (*models.SagaState, error)
	GetByOrderID(ctx context.Context, orderID uuid.UUID) (*models.SagaState, error)
	Update(ctx context.Context, sagaState *models.SagaState) error
	UpdateStatus(ctx context.Context, sagaID uuid.UUID, status models.SagaStatus) error
	UpdateStep(ctx context.Context, sagaID uuid.UUID, step models.SagaStep) error
	UpdateStepAndStatus(ctx context.Context, sagaID uuid.UUID, step models.SagaStep, status models.SagaStatus) error
	Delete(ctx context.Context, sagaID uuid.UUID) error
	List(ctx context.Context, limit, offset int) ([]*models.SagaState, error)
	GetByStatus(ctx context.Context, status models.SagaStatus, limit, offset int) ([]*models.SagaState, error)
	FindRetryable(ctx context.Context, now time.Time) ([]models.SagaState, error)
}

type sagaRepository struct {
	db *gorm.DB
}

func NewSagaRepository(db *gorm.DB) SagaRepository {
	return &sagaRepository{db: db}
}

func (r *sagaRepository) Create(ctx context.Context, sagaState *models.SagaState) error {
	sagaState.StartedAt = time.Now()
	sagaState.UpdatedAt = time.Now()
	return r.db.WithContext(ctx).Create(sagaState).Error
}

func (r *sagaRepository) GetByID(ctx context.Context, sagaID uuid.UUID) (*models.SagaState, error) {
	var sagaState models.SagaState
	err := r.db.WithContext(ctx).First(&sagaState, "saga_id = ?", sagaID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.ErrSagaNotFound
	} else if err != nil {
		return nil, err
	}

	return &sagaState, nil
}

func (r *sagaRepository) GetByOrderID(ctx context.Context, orderID uuid.UUID) (*models.SagaState, error) {
	var sagaState models.SagaState
	err := r.db.WithContext(ctx).First(&sagaState, "order_id = ?", orderID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.ErrSagaNotFound
	} else if err != nil {
		return nil, err
	}

	return &sagaState, nil
}

func (r *sagaRepository) Update(ctx context.Context, sagaState *models.SagaState) error {
	sagaState.UpdatedAt = time.Now()
	return r.db.WithContext(ctx).
		Model(&models.SagaState{}).
		Where("saga_id = ?", sagaState.SagaID).
		Updates(sagaState).Error
}

func (r *sagaRepository) UpdateStatus(ctx context.Context, sagaID uuid.UUID, status models.SagaStatus) error {
	return r.db.WithContext(ctx).
		Model(&models.SagaState{}).
		Where("saga_id = ?", sagaID).
		Updates(map[string]any{
			"status":     status,
			"updated_at": time.Now(),
		}).Error
}

func (r *sagaRepository) UpdateStep(ctx context.Context, sagaID uuid.UUID, step models.SagaStep) error {
	return r.db.WithContext(ctx).
		Model(&models.SagaState{}).
		Where("saga_id = ?", sagaID).
		Updates(map[string]any{
			"current_step": step,
			"updated_at":   time.Now(),
		}).Error
}

func (r *sagaRepository) UpdateStepAndStatus(ctx context.Context, sagaID uuid.UUID, step models.SagaStep, status models.SagaStatus) error {
	return r.db.WithContext(ctx).
		Model(&models.SagaState{}).
		Where("saga_id = ?", sagaID).
		Updates(map[string]any{
			"current_step": step,
			"status":       status,
			"updated_at":   time.Now(),
		}).Error
}

func (r *sagaRepository) Delete(ctx context.Context, sagaID uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.SagaState{}, "saga_id = ?", sagaID).Error
}

func (r *sagaRepository) List(ctx context.Context, limit, offset int) ([]*models.SagaState, error) {
	var sagaStates []*models.SagaState
	err := r.db.WithContext(ctx).
		Order("started_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&sagaStates).Error
	if err != nil {
		return nil, err
	}
	return sagaStates, nil
}

func (r *sagaRepository) FindRetryable(ctx context.Context, now time.Time) ([]models.SagaState, error) {
	var sagaStates []models.SagaState
	err := r.db.WithContext(ctx).
		Where("status IN ? AND (next_retry_at IS NULL OR next_retry_at <= ?)",
			[]models.SagaStatus{models.SagaCompensating, models.SagaCompensationFailed},
			now,
		).
		Find(&sagaStates).Error
	if err != nil {
		return nil, err
	}
	return sagaStates, nil
}

func (r *sagaRepository) GetByStatus(ctx context.Context, status models.SagaStatus, limit, offset int) ([]*models.SagaState, error) {
	var sagaStates []*models.SagaState
	err := r.db.WithContext(ctx).
		Where("status = ?", status).
		Order("started_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&sagaStates).Error
	if err != nil {
		return nil, err
	}
	return sagaStates, nil
}
