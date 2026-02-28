//go:build integration

package repository

import (
	"testing"
	"time"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"gorm.io/gorm"
)

func cleanupTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	tables := []string{
		"inventory_reservations",
		"order_items",
		"payments",
		"saga_states",
		"idempotency_keys",
		"orders",
		"products",
	}
	for _, table := range tables {
		if err := db.Exec("TRUNCATE TABLE " + table + " CASCADE").Error; err != nil {
			t.Fatalf("failed to truncate table %s: %s", table, err)
		}
	}
}

func newTestProduct(overrides ...func(*models.Product)) *models.Product {
	p := &models.Product{
		ID:          uuid.New(),
		Name:        "Test Product",
		Description: "A test product",
		Price:       29.99,
		Stock:       100,
	}
	for _, fn := range overrides {
		fn(p)
	}
	return p
}

func newTestOrder(overrides ...func(*models.Order)) *models.Order {
	o := &models.Order{
		ID:          uuid.New(),
		PublicID:    uuid.NewString(),
		CustomerID:  "customer-" + uuid.NewString()[:8],
		TotalAmount: 0,
		Status:      models.OrderPending,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	for _, fn := range overrides {
		fn(o)
	}
	return o
}

func newTestPayment(orderID uuid.UUID, overrides ...func(*models.Payment)) *models.Payment {
	p := &models.Payment{
		ID:        uuid.New(),
		OrderID:   orderID,
		Amount:    99.99,
		Status:    models.PaymentPending,
		CreatedAt: time.Now().UTC(),
	}
	for _, fn := range overrides {
		fn(p)
	}
	return p
}

func newTestSagaState(orderID uuid.UUID, overrides ...func(*models.SagaState)) *models.SagaState {
	s := &models.SagaState{
		SagaID:      uuid.New(),
		OrderID:     orderID,
		Status:      models.SagaStatusStarted,
		CurrentStep: models.StepReserveInventory,
		Payload:     sonic.NoCopyRawMessage(`{"test": true}`),
		StartedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	for _, fn := range overrides {
		fn(s)
	}
	return s
}

func newTestIdempotencyKey(overrides ...func(*models.IdempotencyKey)) *models.IdempotencyKey {
	k := &models.IdempotencyKey{
		Key:       "idem-" + uuid.NewString()[:8],
		Response:  sonic.NoCopyRawMessage(`{"status": "ok"}`),
		CreatedAt: time.Now().UTC(),
	}
	for _, fn := range overrides {
		fn(k)
	}
	return k
}
