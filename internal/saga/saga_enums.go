package saga

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
