package orchestrator

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/mateusmlo/altimit-ecomm/internal/config"
	"github.com/mateusmlo/altimit-ecomm/internal/errs"
	"github.com/mateusmlo/altimit-ecomm/internal/kafka"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"github.com/mateusmlo/altimit-ecomm/internal/repository"
)

const (
	InitialBackoff         = 1 * time.Second
	MaxBackoff             = 60 * time.Second
	BackoffMultiplier      = 2.0
	MaxCompensationRetries = 3
)

type SagaOrchestrator interface {
	StartSaga(ctx context.Context, order *models.Order) error
	ProcessStepSuccess(ctx context.Context, sagaID uuid.UUID) error
	ProcessStepFailure(ctx context.Context, sagaID uuid.UUID) error
	ProcessCompensationSuccess(ctx context.Context, sagaID uuid.UUID) error
	ProcessCompensationFailure(ctx context.Context, sagaID uuid.UUID) error
	GetSagaState(ctx context.Context, sagaID uuid.UUID) (*models.SagaState, error)
	RetryWorker(ctx context.Context)
}

type orchestrator struct {
	sagaRepo  repository.SagaRepository
	orderRepo repository.OrderRepository
	producer  kafka.EventPublisher
	cfg       *config.Config
}

func NewOrchestrator(
	sagaRepo repository.SagaRepository,
	orderRepo repository.OrderRepository,
	producer kafka.EventPublisher,
	cfg *config.Config,
) SagaOrchestrator {
	return &orchestrator{
		sagaRepo:  sagaRepo,
		orderRepo: orderRepo,
		producer:  producer,
		cfg:       cfg,
	}
}

func (o *orchestrator) StartSaga(ctx context.Context, order *models.Order) error {
	p, err := sonic.Marshal(order)
	if err != nil {
		return err
	}

	newState := models.SagaState{
		OrderID:     order.ID,
		Status:      models.SagaStarted,
		CurrentStep: models.StepReserveInventory,
		Payload:     p,
	}
	err = o.sagaRepo.Create(ctx, &newState)
	if err != nil {
		return err
	}

	err = o.orderRepo.UpdateStatus(ctx, order.ID, models.OrderProcessing)
	if err != nil {
		return err
	}

	cmd, err := o.buildCommandForStep(newState.CurrentStep, order)
	if err != nil {
		return err
	}

	cmdPayload, err := sonic.Marshal(cmd)
	if err != nil {
		return err
	}

	ev := models.Event{
		Event:     models.EventReserveInventory,
		EventID:   uuid.New(),
		SagaID:    newState.SagaID,
		OrderID:   order.ID,
		Payload:   cmdPayload,
		Timestamp: time.Now().Unix(),
	}

	return o.producer.PublishEvent(ctx, o.cfg.Topics.Commands.Inventory, ev)
}

func (o *orchestrator) GetSagaState(ctx context.Context, sagaID uuid.UUID) (*models.SagaState, error) {
	return o.sagaRepo.GetByID(ctx, sagaID)
}

func (o *orchestrator) ProcessStepSuccess(ctx context.Context, sagaID uuid.UUID) error {
	workflow := models.GetOrderWorkflow()
	saga, err := o.sagaRepo.GetByID(ctx, sagaID)
	if err != nil {
		return err
	}

	order, err := o.orderRepo.GetByID(ctx, saga.OrderID)
	if err != nil {
		return err
	}

	stepIdx := findStepIndex(workflow, saga.CurrentStep)
	if stepIdx+1 >= len(workflow.Steps) {
		err := o.sagaRepo.UpdateStatus(ctx, sagaID, models.SagaCompleted)
		if err != nil {
			return err
		}

		return o.orderRepo.UpdateStatus(ctx, order.ID, models.OrderCompleted)
	}

	nextStep := workflow.Steps[stepIdx+1]
	cmd, err := o.buildCommandForStep(nextStep.Step, order)
	if err != nil {
		return err
	}

	cmdPayload, err := sonic.Marshal(cmd)
	if err != nil {
		return err
	}

	ev := models.Event{
		Event:     nextStep.EventType,
		EventID:   uuid.New(),
		SagaID:    saga.SagaID,
		OrderID:   order.ID,
		Payload:   cmdPayload,
		Timestamp: time.Now().Unix(),
	}

	err = o.sagaRepo.UpdateStepAndStatus(ctx, sagaID, nextStep.Step, models.SagaInProgress)
	if err != nil {
		return err
	}

	return o.producer.PublishEvent(ctx, nextStep.CommandTopic, ev)
}

func (o *orchestrator) ProcessStepFailure(ctx context.Context, sagaID uuid.UUID) error {
	workflow := models.GetOrderWorkflow()
	saga, err := o.sagaRepo.GetByID(ctx, sagaID)
	if err != nil {
		return err
	}

	order, err := o.orderRepo.GetByID(ctx, saga.OrderID)
	if err != nil {
		return err
	}

	stepIdx := findStepIndex(workflow, saga.CurrentStep)
	completedSteps := slices.Clone(workflow.Steps[:stepIdx+1])

	if len(completedSteps) == 0 {
		err = o.sagaRepo.UpdateStatus(ctx, sagaID, models.SagaFailed)
		if err != nil {
			return err
		}

		return o.orderRepo.UpdateStatus(ctx, order.ID, models.OrderFailed)
	}

	var firstCompensationStep *models.StepDefinition
	for i := len(completedSteps) - 1; i >= 0; i-- {
		if completedSteps[i].CompensationStep != nil {
			firstCompensationStep = &completedSteps[i]
			break
		}
	}

	if firstCompensationStep == nil {
		err = o.sagaRepo.UpdateStatus(ctx, sagaID, models.SagaFailed)
		if err != nil {
			return err
		}

		return o.orderRepo.UpdateStatus(ctx, order.ID, models.OrderFailed)
	}

	return o.sendCompensationCommand(ctx, saga, order, firstCompensationStep)
}

func (o *orchestrator) ProcessCompensationSuccess(ctx context.Context, sagaID uuid.UUID) error {
	workflow := models.GetOrderWorkflow()
	saga, err := o.sagaRepo.GetByID(ctx, sagaID)
	if err != nil {
		return err
	}

	order, err := o.orderRepo.GetByID(ctx, saga.OrderID)
	if err != nil {
		return err
	}

	currentCompensationStepIdx := findCompensationStepIndex(workflow, saga.CurrentStep)
	nextCompensationStep := findNextCompensationStep(workflow, currentCompensationStepIdx)

	if nextCompensationStep == nil {
		err = o.sagaRepo.UpdateStatus(ctx, sagaID, models.SagaCompensated)
		if err != nil {
			return err
		}
		return o.orderRepo.UpdateStatus(ctx, order.ID, models.OrderFailed)
	}

	return o.sendCompensationCommand(ctx, saga, order, nextCompensationStep)
}

func (o *orchestrator) ProcessCompensationFailure(ctx context.Context, sagaID uuid.UUID) error {
	saga, err := o.sagaRepo.GetByID(ctx, sagaID)
	if err != nil {
		return err
	}

	order, err := o.orderRepo.GetByID(ctx, saga.OrderID)
	if err != nil {
		return err
	}

	saga.CompensationRetries++

	if saga.CompensationRetries < MaxCompensationRetries {
		backoff := calculateBackoff(saga.CompensationRetries)
		nextRetry := time.Now().Add(backoff)
		saga.NextRetryAt = &nextRetry

		log.Printf(
			"Compensation failed for SAGA %s, attempt %d/%d. Next retry at %s",
			sagaID,
			saga.CompensationRetries,
			MaxCompensationRetries,
			nextRetry.Format(time.RFC3339),
		)

		return o.sagaRepo.Update(ctx, saga)
	}

	log.Printf(
		"CRITICAL: Compensation failed for SAGA %s after %d attempts. Manual intervention required!",
		sagaID,
		saga.CompensationRetries,
	)

	err = o.sagaRepo.UpdateStatus(ctx, sagaID, models.SagaCompensationFailed)
	if err != nil {
		return err
	}

	err = o.orderRepo.UpdateStatus(ctx, order.ID, models.OrderFailed)
	if err != nil {
		return err
	}

	// TODO: Alert in monitoring channels
	// TODO: Publish to DLQ

	return fmt.Errorf("%w: saga %s at step %s after %d retries",
		errs.ErrCompensationFailed,
		sagaID,
		saga.CurrentStep,
		saga.CompensationRetries,
	)
}

func (o *orchestrator) sendCompensationCommand(
	ctx context.Context,
	saga *models.SagaState,
	order *models.Order,
	stepToCompensate *models.StepDefinition,
) error {
	cmd, err := o.buildCommandForStep(*stepToCompensate.CompensationStep, order)
	if err != nil {
		return err
	}

	cmdPayload, err := sonic.Marshal(cmd)
	if err != nil {
		return err
	}

	ev := models.Event{
		Event:     stepToCompensate.CompensationEventType,
		EventID:   uuid.New(),
		SagaID:    saga.SagaID,
		OrderID:   order.ID,
		Payload:   cmdPayload,
		Timestamp: time.Now().Unix(),
	}

	err = o.sagaRepo.UpdateStepAndStatus(
		ctx,
		saga.SagaID,
		*stepToCompensate.CompensationStep,
		models.SagaCompensating,
	)
	if err != nil {
		return err
	}

	return o.producer.PublishEvent(ctx, stepToCompensate.CompensationCommandTopic, ev)
}

func orderItemsToInventoryProducts(items []models.OrderItem) []models.InventoryProduct {
	var prods []models.InventoryProduct

	for _, item := range items {
		prods = append(prods, models.InventoryProduct{
			ProductID: item.ItemID,
			Quantity:  item.Quantity,
		})
	}

	return prods
}

func findStepIndex(workflow *models.SagaWorkflow, step models.SagaStep) int {
	return slices.IndexFunc(workflow.Steps, func(s models.StepDefinition) bool {
		return s.Step == step
	})
}

func (o *orchestrator) buildCommandForStep(
	step models.SagaStep,
	order *models.Order,
) (any, error) {
	var cmd any

	switch step {
	case models.StepReserveInventory:
		cmd = &models.ReserveInventoryCommand{
			Products: orderItemsToInventoryProducts(order.Items),
		}

	case models.StepProcessPayment:
		cmd = &models.ProcessPaymentCommand{
			OrderID:  order.ID,
			Amount:   order.TotalAmount,
			Currency: order.Currency,
		}

	case models.StepSendNotification:
		cmd = &models.SendNotificationCommand{
			CustomerID: order.CustomerID,
			OrderID:    order.ID,
			Message:    fmt.Sprintf("Your order %s has been confirmed!", order.PublicID),
		}

	case models.StepCompensateInventory:
		cmd = &models.ReleaseInventoryCommand{
			Products: orderItemsToInventoryProducts(order.Items),
		}

	case models.StepCompensatePayment:
		cmd = &models.RefundPaymentCommand{
			OrderID: order.ID,
		}

	default:
		return nil, fmt.Errorf("unknown step: %s", step)
	}

	return cmd, nil
}

func findCompensationStepIndex(workflow *models.SagaWorkflow, compensationStep models.SagaStep) int {
	return slices.IndexFunc(workflow.Steps, func(s models.StepDefinition) bool {
		return s.CompensationStep != nil && *s.CompensationStep == compensationStep
	})
}

func findNextCompensationStep(workflow *models.SagaWorkflow, currentIdx int) *models.StepDefinition {
	for i := currentIdx - 1; i >= 0; i-- {
		if workflow.Steps[i].CompensationStep != nil {
			return &workflow.Steps[i]
		}
	}

	return nil
}

func calculateBackoff(attempt int) time.Duration {
	backoff := float64(InitialBackoff) * math.Pow(BackoffMultiplier, float64(attempt-1))

	if backoff > float64(MaxBackoff) {
		backoff = float64(MaxBackoff)
	}

	jitter := backoff * 0.2 * (rand.Float64()*2 - 1)

	return time.Duration(backoff + jitter)
}

// Run this in a goroutine when orchestrator starts
func (o *orchestrator) RetryWorker(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sagas, err := o.sagaRepo.FindRetryable(ctx, time.Now())
			if err != nil {
				log.Printf("Error finding retryable SAGAs: %v", err)
				continue
			}

			for _, saga := range sagas {
				order, _ := o.orderRepo.GetByID(ctx, saga.OrderID)
				workflow := models.GetOrderWorkflow()
				stepIdx := findCompensationStepIndex(workflow, saga.CurrentStep)

				if stepIdx >= 0 {
					o.sendCompensationCommand(ctx, &saga, order, &workflow.Steps[stepIdx])
				}
			}
		case <-ctx.Done():
			return
		}
	}
}
