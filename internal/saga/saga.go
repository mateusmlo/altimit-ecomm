package saga

import (
	"time"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
)

type SagaState struct {
	SagaID      uuid.UUID              `json:"saga_id"`
	OrderID     uuid.UUID              `json:"order_id"`
	Status      SagaStatus             `json:"status"`
	CurrentStep SagaStep               `json:"current_step"`
	Payload     sonic.NoCopyRawMessage `json:"payload"`
	StartedAt   time.Time              `json:"started_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
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
