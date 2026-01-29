package models

import (
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
	EventInventoryReserved  EventType = "INVENTORY_RESERVED"
	EventInventoryFailed    EventType = "INVENTORY_FAILED"
	EventPaymentProcessed   EventType = "PAYMENT_PROCESSED"
	EventPaymentFailed      EventType = "PAYMENT_FAILED"
	EventNotificationSent   EventType = "NOTIFICATION_SENT"
	EventNotificationFailed EventType = "NOTIFICATION_FAILED"
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
type InventoryItem struct {
	ItemID   uuid.UUID `json:"item_id"`
	Quantity int       `json:"quantity"`
}

// Command payloads
type ReserveInventoryCommand struct {
	Items []InventoryItem `json:"items"`
}

type ReleaseInventoryCommand struct {
	Items []InventoryItem `json:"items"`
}

type ProcessPaymentCommand struct {
	Amount     float64 `json:"amount"`
	CustomerID string  `json:"customer_id"`
}

type RefundPaymentCommand struct {
	PaymentID string  `json:"payment_id"`
	Amount    float64 `json:"amount"`
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
	Success   bool   `json:"success"`
	PaymentID string `json:"payment_id,omitempty"`
	Message   string `json:"message"`
}

type NotificationReply struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}
