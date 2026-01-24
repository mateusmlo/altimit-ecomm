package models

// OrderStatus represents the status of an order.
type OrderStatus string

const (
	OrderPending    OrderStatus = "PENDING"
	OrderProcessing OrderStatus = "PROCESSING"
	OrderCompleted  OrderStatus = "COMPLETED"
	OrderCancelled  OrderStatus = "CANCELLED"
	OrderFailed     OrderStatus = "FAILED"
)
