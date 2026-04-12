package models

import (
	"fmt"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
)

type EventType string

const (
	// Commands (requests to services)
	EventReserveInventory EventType = "RESERVE_INVENTORY"
	EventReleaseInventory EventType = "RELEASE_INVENTORY"
	EventProcessPayment   EventType = "PROCESS_PAYMENT"
	EventRefundPayment    EventType = "REFUND_PAYMENT"
	EventSendNotification EventType = "SEND_NOTIFICATION"

	// Replies (responses from services)
	EventInventoryReserved      EventType = "INVENTORY_RESERVED"
	EventInventoryReleased      EventType = "INVENTORY_RELEASED"
	EventReserveInventoryFailed EventType = "RESERVE_INVENTORY_FAILED"
	EventReleaseInventoryFailed EventType = "RELEASE_INVENTORY_FAILED"
	EventPaymentSucceeded       EventType = "PAYMENT_SUCCEEDED"
	EventPaymentFailed          EventType = "PAYMENT_FAILED"
	EventRefundSucceeded        EventType = "REFUND_SUCCEEDED"
	EventRefundFailed           EventType = "REFUND_FAILED"
	EventNotificationSent       EventType = "NOTIFICATION_SENT"
	EventNotificationFailed     EventType = "NOTIFICATION_FAILED"
)

type Event struct {
	Event     EventType              `json:"event"`
	EventID   uuid.UUID              `json:"event_id"`
	SagaID    uuid.UUID              `json:"saga_id"`
	OrderID   uuid.UUID              `json:"order_id"`
	Timestamp int64                  `json:"timestamp"`
	Payload   sonic.NoCopyRawMessage `json:"payload"`
}

// Comes from OrderItem struct
type InventoryProduct struct {
	ProductID uuid.UUID `json:"product_id"`
	Quantity  int       `json:"quantity"`
}

// Command payloads
type ReserveInventoryCommand struct {
	Products []InventoryProduct `json:"products"`
}

type ReleaseInventoryCommand struct {
	Products []InventoryProduct `json:"products"`
}

type ProcessPaymentCommand struct {
	OrderID  uuid.UUID `json:"order_id"`
	Amount   float64   `json:"amount"`
	Currency string    `json:"currency"`
}

type RefundPaymentCommand struct {
	OrderID uuid.UUID `json:"order_id"`
}

type SendNotificationCommand struct {
	CustomerID string    `json:"customer_id"`
	OrderID    uuid.UUID `json:"order_id"`
	Message    string    `json:"message"`
}

// Reply payloads
type InventoryReply struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type PaymentReply struct {
	Success         bool   `json:"success"`
	PaymentIntentID string `json:"payment_intent_id,omitempty"`
	Message         string `json:"message"`
}

type NotificationReply struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func ValidateInventoryCommand(prods []InventoryProduct) *InventoryReply {
	if len(prods) == 0 {
		return &InventoryReply{
			Success: false,
			Message: "No products specified",
		}
	}

	for _, product := range prods {
		if product.Quantity <= 0 {
			return &InventoryReply{
				Success: false,
				Message: fmt.Sprintf("Invalid quantity for product %s", product.ProductID.String()),
			}
		}
	}

	return nil
}
