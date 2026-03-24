package models

import (
	"time"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SagaStatus string
type SagaStep string

const (
	SagaStarted            SagaStatus = "STARTED"
	SagaCompleted          SagaStatus = "COMPLETED"
	SagaInProgress         SagaStatus = "IN_PROGRESS"
	SagaCancelled          SagaStatus = "CANCELLED"
	SagaFailed             SagaStatus = "FAILED"
	SagaCompensated        SagaStatus = "COMPENSATED"
	SagaCompensating       SagaStatus = "COMPENSATING"
	SagaCompensationFailed SagaStatus = "COMPENSATION_FAILED"

	StepReserveInventory    SagaStep = "RESERVE_INVENTORY"
	StepProcessPayment      SagaStep = "PROCESS_PAYMENT"
	StepSendNotification    SagaStep = "SEND_NOTIFICATION"
	StepCompensatePayment   SagaStep = "COMPENSATE_PAYMENT"
	StepCompensateInventory SagaStep = "COMPENSATE_INVENTORY"
)

type SagaState struct {
	SagaID              uuid.UUID              `json:"saga_id" gorm:"type:uuid;primaryKey"`
	OrderID             uuid.UUID              `json:"order_id" gorm:"type:uuid;not null;index"`
	Status              SagaStatus             `json:"status" gorm:"type:saga_status;not null;default:STARTED;index"`
	CurrentStep         SagaStep               `json:"current_step" gorm:"type:varchar(50);not null"`
	CompensationRetries int                    `json:"compensation_retries" gorm:"default:0"`
	NextRetryAt         *time.Time             `json:"next_retry_at" gorm:"type:timestamptz"`
	Payload             sonic.NoCopyRawMessage `json:"payload" gorm:"type:jsonb"`
	StartedAt           time.Time              `json:"started_at" gorm:"not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt           time.Time              `json:"updated_at" gorm:"not null;default:CURRENT_TIMESTAMP"`
}

func (s *SagaState) BeforeCreate(tx *gorm.DB) error {
	s.SagaID = uuid.New()
	return nil
}

type SagaWorkflow struct {
	Steps []StepDefinition
}

type StepDefinition struct {
	Step                  SagaStep
	EventType             EventType
	CommandTopic          string
	ReplyTopic            string
	CompensationStep         *SagaStep
	CompensationEventType    EventType
	CompensationCommandTopic string
}

func GetOrderWorkflow() *SagaWorkflow {
	return &SagaWorkflow{
		Steps: []StepDefinition{
			{
				Step:             StepReserveInventory,
				EventType:        EventReserveInventory,
				CommandTopic:     "inventory.commands",
				ReplyTopic:       "inventory.replies",
				CompensationStep: nil,
			},
			{
				Step:                     StepProcessPayment,
				EventType:                EventProcessPayment,
				CommandTopic:             "payment.commands",
				ReplyTopic:               "payment.replies",
				CompensationStep:         ptrTo(StepCompensateInventory),
				CompensationEventType:    EventReleaseInventory,
				CompensationCommandTopic: "inventory.commands",
			},
			{
				Step:                     StepSendNotification,
				EventType:                EventSendNotification,
				CommandTopic:             "notification.commands",
				ReplyTopic:               "notification.replies",
				CompensationStep:         ptrTo(StepCompensatePayment),
				CompensationEventType:    EventRefundPayment,
				CompensationCommandTopic: "payment.commands",
			},
		},
	}
}

func ptrTo[T any](value T) *T {
	return &value
}
