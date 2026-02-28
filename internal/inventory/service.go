package inventory

import (
	"context"
	"errors"
	"fmt"

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

	switch {
	case errors.Is(err, errs.ErrInsufficientStock):
		return &models.InventoryReply{
			Success: false,
			Message: fmt.Sprintf("Insufficient stock: %v", err),
		}, nil

	case errors.Is(err, errs.ErrProductNotFound):
		return &models.InventoryReply{
			Success: false,
			Message: fmt.Sprintf("Product not found: %v", err),
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

	switch {
	case errors.Is(err, errs.ErrProductNotFound):
		return &models.InventoryReply{
			Success: false,
			Message: fmt.Sprintf("Product not found: %v", err),
		}, nil

	case errors.Is(err, errs.ErrNoActiveReservations):
		return &models.InventoryReply{
			Success: false,
			Message: fmt.Sprintf("No active reservations found: %v", err),
		}, nil

	case err != nil:
		return nil, err
	}

	return &models.InventoryReply{
		Success: true,
		Message: "Inventory released successfully",
	}, nil
}
