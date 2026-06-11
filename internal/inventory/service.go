package inventory

import (
	"context"
	"errors"
	"log"

	"github.com/google/uuid"
	"github.com/mateusmlo/altimit-ecomm/internal/errs"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"github.com/mateusmlo/altimit-ecomm/internal/repository"
)

type InventoryService struct {
	repo repository.InventoryReservationRepository
}

func NewInventoryService(repo repository.InventoryReservationRepository) *InventoryService {
	return &InventoryService{
		repo: repo,
	}
}

func (s *InventoryService) ReserveInventory(ctx context.Context, orderID uuid.UUID, cmd models.ReserveInventoryCommand) (*models.InventoryReply, error) {
	if invalidCmd := models.ValidateInventoryCommand(cmd.Products); invalidCmd != nil {
		return invalidCmd, nil
	}

	err := s.repo.ReserveItems(ctx, orderID, cmd.Products)

	var stockErr *errs.InsufficientStockError
	switch {
	case errors.As(err, &stockErr):
		log.Printf("insufficient stock order_id=%s product_id=%s requested=%d available=%d",
			orderID, stockErr.ProductID, stockErr.Requested, stockErr.Available)
		return &models.InventoryReply{
			Success: false,
			Message: "Insufficient stock",
		}, nil

	case errors.Is(err, errs.ErrProductNotFound):
		return &models.InventoryReply{
			Success: false,
			Message: "Product not found",
		}, nil

	case err != nil:
		return nil, err
	}

	return &models.InventoryReply{
		Success: true,
		Message: "Inventory reserved successfully",
	}, nil
}

func (s *InventoryService) ReleaseInventory(ctx context.Context, orderID uuid.UUID, cmd models.ReleaseInventoryCommand) (*models.InventoryReply, error) {
	err := s.repo.ReleaseItems(ctx, orderID, cmd.Products)

	var noResvErr *errs.NoActiveReservationsError
	var mismatchErr *errs.QuantityMismatchError
	switch {
	case errors.Is(err, errs.ErrProductNotFound):
		return &models.InventoryReply{
			Success: false,
			Message: "Product not found",
		}, nil

	case errors.As(err, &noResvErr):
		log.Printf("no active reservations order_id=%s product_id=%s",
			noResvErr.OrderID, noResvErr.ProductID)
		return &models.InventoryReply{
			Success: false,
			Message: "No active reservations",
		}, nil

	case errors.As(err, &mismatchErr):
		log.Printf("quantity mismatch order_id=%s product_id=%s reserved=%d requested=%d",
			orderID, mismatchErr.ProductID, mismatchErr.ReservationQuantity, mismatchErr.ItemQuantity)
		return &models.InventoryReply{
			Success: false,
			Message: "Quantity mismatch",
		}, nil

	case err != nil:
		return nil, err
	}

	return &models.InventoryReply{
		Success: true,
		Message: "Inventory released successfully",
	}, nil
}
