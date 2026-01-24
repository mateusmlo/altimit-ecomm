package saga

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type SagaState struct {
	SagaID      uuid.UUID       `json:"saga_id"`
	OrderID     uuid.UUID       `json:"order_id"`
	Status      SagaStatus      `json:"status"`
	CurrentStep SagaStep        `json:"current_step"`
	Payload     json.RawMessage `json:"payload"`
	StartedAt   time.Time       `json:"started_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}
