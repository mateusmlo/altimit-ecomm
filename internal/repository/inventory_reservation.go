package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"gorm.io/gorm"
)

type InventoryReservationRepository interface {
	ReserveItems(ctx context.Context, orderID uuid.UUID, items []models.InventoryProduct) error
	ReleaseItems(ctx context.Context, orderID uuid.UUID, items []models.InventoryProduct) error
	ConfirmByOrderID(ctx context.Context, orderID uuid.UUID) error
	FindByOrderID(ctx context.Context, orderID uuid.UUID) ([]models.InventoryReservation, error)
	FindByProductID(ctx context.Context, productID uuid.UUID) ([]models.InventoryReservation, error)
}

type inventoryReservationRepository struct {
	db *gorm.DB
}

func NewInventoryReservationRepository(db *gorm.DB) InventoryReservationRepository {
	return &inventoryReservationRepository{db: db}
}

// ReserveItems decrements product stock and creates ACTIVE reservation records atomically.
func (r *inventoryReservationRepository) ReserveItems(ctx context.Context, orderID uuid.UUID, items []models.InventoryProduct) error {
	if len(items) == 0 {
		return nil
	}

	var reservations []models.InventoryReservation

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		itemIDs := extractItemIDs(items)

		products, err := lockAndFetchProducts(tx, itemIDs)
		if err != nil {
			return err
		}

		itemQty := make(map[uuid.UUID]int, len(items))
		for _, item := range items {
			itemQty[item.ProductID] = item.Quantity
		}

		for _, p := range products {
			if requestedQty := itemQty[p.ID]; p.Stock < requestedQty {
				return fmt.Errorf("%w for product %s: available=%d, requested=%d",
					ErrInsufficientStock, p.ID, p.Stock, requestedQty)
			}
		}

		if err := batchUpdateProductStock(tx, MINUS_OP, items); err != nil {
			return err
		}

		now := time.Now().UTC()
		reservations = make([]models.InventoryReservation, len(items))
		for i, item := range items {
			reservations[i] = models.InventoryReservation{
				ID:        uuid.New(),
				OrderID:   orderID,
				ProductID: item.ProductID,
				Quantity:  item.Quantity,
				Status:    models.ReservationActive,
				CreatedAt: now,
				UpdatedAt: now,
			}
		}

		return tx.Create(&reservations).Error
	})

	if err != nil {
		return err
	}

	return nil
}

func (r *inventoryReservationRepository) ReleaseItems(ctx context.Context, orderID uuid.UUID, items []models.InventoryProduct) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, item := range items {
			var reservation models.InventoryReservation
			err := tx.Where("order_id = ? AND product_id = ? AND status = ?",
				orderID, item.ProductID, models.ReservationActive).
				First(&reservation).Error

			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return fmt.Errorf("%w: product %s for order %s",
						ErrNoActiveReservations, item.ProductID, orderID)
				}
				return err
			}

			if reservation.Quantity != item.Quantity {
				return fmt.Errorf("quantity mismatch for product %s: reserved=%d, requested=%d",
					item.ProductID, reservation.Quantity, item.Quantity)
			}
		}

		if err := batchUpdateProductStock(tx, PLUS_OP, items); err != nil {
			return err
		}

		productIDs := extractItemIDs(items)
		return tx.Model(&models.InventoryReservation{}).
			Where("order_id = ? AND product_id IN ? AND status = ?",
				orderID, productIDs, models.ReservationActive).
			Updates(map[string]any{
				"status":     models.ReservationReleased,
				"updated_at": time.Now().UTC(),
			}).Error
	})
}

// ConfirmByOrderID marks ACTIVE reservations as CONFIRMED. No stock changes needed
// because stock was already decremented at reserve time.
func (r *inventoryReservationRepository) ConfirmByOrderID(ctx context.Context, orderID uuid.UUID) error {
	result := r.db.WithContext(ctx).
		Model(&models.InventoryReservation{}).
		Where("order_id = ? AND status = ?", orderID, models.ReservationActive).
		Updates(map[string]any{
			"status":     models.ReservationConfirmed,
			"updated_at": time.Now().UTC(),
		})

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("%w for order %s", ErrNoActiveReservations, orderID)
	}

	return nil
}

// FindByOrderID retrieves all reservations for a given order.
func (r *inventoryReservationRepository) FindByOrderID(ctx context.Context, orderID uuid.UUID) ([]models.InventoryReservation, error) {
	var reservations []models.InventoryReservation
	err := r.db.WithContext(ctx).
		Where("order_id = ?", orderID).
		Find(&reservations).Error
	if err != nil {
		return nil, err
	}

	return reservations, nil
}

// FindByProductID retrieves all reservations for a given product.
func (r *inventoryReservationRepository) FindByProductID(ctx context.Context, productID uuid.UUID) ([]models.InventoryReservation, error) {
	var reservations []models.InventoryReservation
	err := r.db.WithContext(ctx).
		Where("product_id = ?", productID).
		Find(&reservations).Error
	if err != nil {
		return nil, err
	}

	return reservations, nil
}

func filterActiveReservations(reservations []models.InventoryReservation) []models.InventoryReservation {
	var activeReservations []models.InventoryReservation
	for _, reservation := range reservations {
		if reservation.Status == models.ReservationActive {
			activeReservations = append(activeReservations, reservation)
		}
	}

	return activeReservations
}
