package repository

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/mateusmlo/altimit-ecomm/internal/errs"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	PLUS_OP  = "+"
	MINUS_OP = "-"
)

// extractItemIDs returns ordered item IDs from InventoryProduct slice.
func extractItemIDs(prods []models.InventoryProduct) []uuid.UUID {
	productIDs := make([]uuid.UUID, len(prods))

	for i, product := range prods {
		productIDs[i] = product.ProductID
	}

	return productIDs
}

// lockAndFetchProducts fetches products with a FOR UPDATE lock, ordered by product_id to prevent deadlocks.
func lockAndFetchProducts(tx *gorm.DB, productIDs []uuid.UUID) ([]*models.Product, error) {
	var products []*models.Product
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Order("product_id").
		Find(&products, "product_id IN ?", productIDs).Error; err != nil {
		return nil, err
	}

	if len(products) != len(productIDs) {
		found := make(map[uuid.UUID]bool, len(products))
		for _, p := range products {
			found[p.ID] = true
		}
		for _, id := range productIDs {
			if !found[id] {
				return nil, fmt.Errorf("%w: %s", errs.ErrProductNotFound, id)
			}
		}
	}

	return products, nil
}

// batchUpdateProductStock builds a CASE WHEN expression and executes a single bulk UPDATE on a Product stock.
// The operator determines the direction: "+" for incrementing, "-" for decrementing.
func batchUpdateProductStock(tx *gorm.DB, operator string, prods []models.InventoryProduct) error {
	if len(prods) == 0 {
		return nil
	}

	itemIDs := extractItemIDs(prods)

	var caseExpr strings.Builder
	caseExpr.WriteString("CASE product_id ")
	args := make([]any, 0, len(prods)*3)

	for _, prod := range prods {
		caseExpr.WriteString("WHEN ? THEN stock " + operator + " ? ")
		args = append(args, prod.ProductID, prod.Quantity)
	}
	caseExpr.WriteString("END")

	return tx.Model(&models.Product{}).
		Where("product_id IN ?", itemIDs).
		Update("stock", gorm.Expr(caseExpr.String(), args...)).Error
}
