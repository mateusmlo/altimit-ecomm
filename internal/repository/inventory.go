package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"gorm.io/gorm"
)

// InventoryRepository defines the interface for inventory operations
type InventoryRepository interface {
	Create(ctx context.Context, item *models.Product) error
	GetByID(ctx context.Context, id uuid.UUID) (*models.Product, error)
	GetByIDs(ctx context.Context, ids []uuid.UUID) ([]*models.Product, error)
	Update(ctx context.Context, item *models.Product) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, limit, offset int) ([]*models.Product, error)
	BatchCheckAvailability(ctx context.Context, items []models.InventoryProduct) (map[uuid.UUID]bool, error)
}

type inventoryRepository struct {
	db *gorm.DB
}

// NewInventoryRepository creates a new inventory repository
func NewInventoryRepository(db *gorm.DB) InventoryRepository {
	return &inventoryRepository{db: db}
}

// Create creates a new item in the inventory
func (r *inventoryRepository) Create(ctx context.Context, item *models.Product) error {
	if item.ID == uuid.Nil {
		item.ID = uuid.New()
	}

	return r.db.WithContext(ctx).Create(item).Error
}

// GetByID retrieves an item by its ID
func (r *inventoryRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.Product, error) {
	var item models.Product
	err := r.db.WithContext(ctx).First(&item, "product_id = ?", id).Error

	if err != nil {
		return nil, err
	}

	return &item, nil
}

// GetByIDs retrieves multiple items by their IDs
func (r *inventoryRepository) GetByIDs(ctx context.Context, ids []uuid.UUID) ([]*models.Product, error) {
	var items []*models.Product
	err := r.db.WithContext(ctx).Find(&items, "product_id IN ?", ids).Error

	if err != nil {
		return nil, err
	}

	return items, nil
}

// Update updates an item in the inventory
func (r *inventoryRepository) Update(ctx context.Context, item *models.Product) error {
	return r.db.WithContext(ctx).
		Model(&models.Product{}).
		Where("product_id = ?", item.ID).
		Updates(item).Error
}

// Delete deletes an item from the inventory
func (r *inventoryRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.Product{}, "product_id = ?", id).Error
}

// List retrieves a list of items from the inventory
func (r *inventoryRepository) List(ctx context.Context, limit, offset int) ([]*models.Product, error) {
	var items []*models.Product

	err := r.db.WithContext(ctx).
		Limit(limit).
		Offset(offset).
		Find(&items).Error

	if err != nil {
		return nil, err
	}

	return items, nil
}

// BatchCheckAvailability checks availability for multiple items
func (r *inventoryRepository) BatchCheckAvailability(ctx context.Context, items []models.InventoryProduct) (map[uuid.UUID]bool, error) {
	if len(items) == 0 {
		return make(map[uuid.UUID]bool), nil
	}

	itemIDs := extractItemIDs(items)

	var products []*models.Product
	if err := r.db.WithContext(ctx).
		Find(&products, "product_id IN ?", itemIDs).Error; err != nil {
		return nil, err
	}

	result := make(map[uuid.UUID]bool)
	for _, p := range products {
		var requestedQty int
		for _, item := range items {
			if item.ProductID == p.ID {
				requestedQty = item.Quantity
				break
			}
		}
		result[p.ID] = p.Stock >= requestedQty
	}

	for _, item := range items {
		if _, exists := result[item.ProductID]; !exists {
			result[item.ProductID] = false
		}
	}

	return result, nil
}
