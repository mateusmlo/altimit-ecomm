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
)

const (
	StepReserve           SagaStep = "RESERVE_INVENTORY"
	StepProcessPayment    SagaStep = "PROCESS_PAYMENT"
	StepSendNotification  SagaStep = "SEND_NOTIFICATION"
	StepConfirmOrder      SagaStep = "CONFIRM_ORDER"
	StepComplete          SagaStep = "COMPLETE"
	StepCompensatePayment SagaStep = "COMPENSATE_PAYMENT"
)
