package models

import (
	"time"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
)

type SagaStatus string
type SagaStep string

const (
	SagaStatusStarted     SagaStatus = "STARTED"
	SagaStatusCompleted   SagaStatus = "COMPLETED"
	SagaStatusInProgress  SagaStatus = "IN_PROGRESS"
	SagaStatusCancelled   SagaStatus = "CANCELLED"
	SagaStatusFailed      SagaStatus = "FAILED"
	SagaStatusCompensated SagaStatus = "COMPENSATED"

	StepReserveInventory    SagaStep = "RESERVE_INVENTORY"
	StepProcessPayment      SagaStep = "PROCESS_PAYMENT"
	StepSendNotification    SagaStep = "SEND_NOTIFICATION"
	StepCompensatePayment   SagaStep = "COMPENSATE_PAYMENT"
	StepCompensateInventory SagaStep = "COMPENSATE_INVENTORY"
)

type SagaState struct {
	SagaID      uuid.UUID              `json:"saga_id" gorm:"type:uuid;primaryKey"`
	OrderID     uuid.UUID              `json:"order_id" gorm:"type:uuid;not null;index"`
	Status      SagaStatus             `json:"status" gorm:"type:saga_status;not null;default:STARTED;index"`
	CurrentStep SagaStep               `json:"current_step" gorm:"type:varchar(50);not null"`
	Payload     sonic.NoCopyRawMessage `json:"payload" gorm:"type:jsonb"`
	StartedAt   time.Time              `json:"started_at" gorm:"not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt   time.Time              `json:"updated_at" gorm:"not null;default:CURRENT_TIMESTAMP"`
}

type SagaWorkflow struct {
	Steps []StepDefinition
}

type StepDefinition struct {
	Step             SagaStep
	CommandTopic     string
	ReplyTopic       string
	CompensationStep *SagaStep
}

func GetOrderWorkflow() *SagaWorkflow {
	return &SagaWorkflow{
		Steps: []StepDefinition{
			{
				Step:             StepReserveInventory,
				CommandTopic:     "inventory.commands",
				ReplyTopic:       "inventory.replies",
				CompensationStep: nil,
			},
			{
				Step:             StepProcessPayment,
				CommandTopic:     "payment.commands",
				ReplyTopic:       "payment.replies",
				CompensationStep: ptrTo(StepCompensateInventory),
			},
			{
				Step:             StepSendNotification,
				CommandTopic:     "notification.commands",
				ReplyTopic:       "notification.replies",
				CompensationStep: ptrTo(StepCompensatePayment),
			},
		},
	}
}

func ptrTo[T any](value T) *T {
	return &value
}
