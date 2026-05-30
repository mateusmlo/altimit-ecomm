package inventory

import (
	"context"
	"testing"
	"time"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/mateusmlo/altimit-ecomm/internal/config"
	"github.com/mateusmlo/altimit-ecomm/internal/errs"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

// mockIdempotencyRepo is a hand-written mock for IdempotencyRepository.
type mockIdempotencyRepo struct {
	existsResult bool
	existsErr    error
	createErr    error
	createdKey   string
	createCalled bool
}

func (m *mockIdempotencyRepo) Create(_ context.Context, key *models.IdempotencyKey) error {
	m.createCalled = true
	m.createdKey = key.Key
	return m.createErr
}

func (m *mockIdempotencyRepo) GetByKey(_ context.Context, _ string) (*models.IdempotencyKey, error) {
	return nil, nil
}

func (m *mockIdempotencyRepo) Exists(_ context.Context, _ string) (bool, error) {
	return m.existsResult, m.existsErr
}

func (m *mockIdempotencyRepo) Delete(_ context.Context, _ string) error { return nil }

func (m *mockIdempotencyRepo) DeleteOlderThan(_ context.Context, _ time.Duration) error { return nil }

// newTestHandler builds a Handler with a nil producer.
// Only safe for tests that never reach publishReply.
func newTestHandler(svc *InventoryService) *Handler {
	return &Handler{
		service:         svc,
		producer:        nil,
		idempotencyRepo: &mockIdempotencyRepo{},
		config:          &config.Config{},
	}
}

func makeMetadataHeader(t *testing.T, m models.RecordMetadata) kgo.RecordHeader {
	t.Helper()
	b, err := sonic.Marshal(m)
	require.NoError(t, err)
	return kgo.RecordHeader{Key: "metadata", Value: b}
}

func TestHandleCommand_MissingMetadataHeader(t *testing.T) {
	h := newTestHandler(NewInventoryService(&mockInventoryRepo{}))

	err := h.HandleCommand(context.Background(), &kgo.Record{
		Headers: []kgo.RecordHeader{},
		Value:   []byte("{}"),
	})

	assert.ErrorIs(t, err, errs.ErrMissingMetadata)
}

func TestHandleCommand_InvalidMetadataJSON(t *testing.T) {
	h := newTestHandler(NewInventoryService(&mockInventoryRepo{}))

	err := h.HandleCommand(context.Background(), &kgo.Record{
		Headers: []kgo.RecordHeader{{Key: "metadata", Value: []byte("not-json")}},
		Value:   []byte("{}"),
	})

	assert.ErrorIs(t, err, errs.ErrMalformedPayload)
}

func TestHandleCommand_UnknownEventType(t *testing.T) {
	h := newTestHandler(NewInventoryService(&mockInventoryRepo{}))

	meta := models.RecordMetadata{
		EventType: models.EventType("UNKNOWN_EVENT"),
		EventID:   uuid.New(),
		SagaID:    uuid.New(),
		OrderID:   uuid.New(),
	}

	err := h.HandleCommand(context.Background(), &kgo.Record{
		Headers: []kgo.RecordHeader{makeMetadataHeader(t, meta)},
		Value:   []byte("{}"),
	})

	assert.ErrorIs(t, err, errs.ErrUnknownEvent)
}

func TestHandleCommand_ReserveInventory_InvalidPayload(t *testing.T) {
	h := newTestHandler(NewInventoryService(&mockInventoryRepo{}))

	meta := models.RecordMetadata{
		EventType: models.EventReserveInventory,
		EventID:   uuid.New(),
		SagaID:    uuid.New(),
		OrderID:   uuid.New(),
	}

	err := h.HandleCommand(context.Background(), &kgo.Record{
		Headers: []kgo.RecordHeader{makeMetadataHeader(t, meta)},
		Value:   []byte("not-json"),
	})

	assert.ErrorIs(t, err, errs.ErrMalformedPayload)
}

func TestHandleCommand_ReleaseInventory_InvalidPayload(t *testing.T) {
	h := newTestHandler(NewInventoryService(&mockInventoryRepo{}))

	meta := models.RecordMetadata{
		EventType: models.EventReleaseInventory,
		EventID:   uuid.New(),
		SagaID:    uuid.New(),
		OrderID:   uuid.New(),
	}

	err := h.HandleCommand(context.Background(), &kgo.Record{
		Headers: []kgo.RecordHeader{makeMetadataHeader(t, meta)},
		Value:   []byte("not-json"),
	})

	assert.ErrorIs(t, err, errs.ErrMalformedPayload)
}

// mockPublisher records the last published event for assertions.
type mockPublisher struct {
	publishErr error
	lastTopic  string
	lastEvent  models.Event
}

func (m *mockPublisher) PublishEvent(_ context.Context, topic string, ev models.Event) error {
	m.lastTopic = topic
	m.lastEvent = ev
	return m.publishErr
}

func TestHandleCommand_DuplicateSuppressed(t *testing.T) {
	repo := &mockInventoryRepo{}
	idempotency := &mockIdempotencyRepo{existsResult: true}

	h := &Handler{
		service:         NewInventoryService(repo),
		producer:        nil, // must not be reached
		idempotencyRepo: idempotency,
		config:          &config.Config{},
	}

	meta := models.RecordMetadata{
		EventType: models.EventReserveInventory,
		EventID:   uuid.New(),
		SagaID:    uuid.New(),
		OrderID:   uuid.New(),
	}

	err := h.HandleCommand(context.Background(), &kgo.Record{
		Headers: []kgo.RecordHeader{makeMetadataHeader(t, meta)},
		Value:   []byte("{}"),
	})

	require.NoError(t, err)
	assert.False(t, repo.reserveCalled, "service must not run for a duplicate command")
	assert.False(t, idempotency.createCalled, "no key should be stored for a duplicate")
}

func TestHandleCommand_StoresKeyAfterSuccess(t *testing.T) {
	repo := &mockInventoryRepo{}
	idempotency := &mockIdempotencyRepo{existsResult: false}

	h := &Handler{
		service:         NewInventoryService(repo),
		producer:        &mockPublisher{},
		idempotencyRepo: idempotency,
		config:          &config.Config{},
	}

	eventID := uuid.New()
	meta := models.RecordMetadata{
		EventType: models.EventReserveInventory,
		EventID:   eventID,
		SagaID:    uuid.New(),
		OrderID:   uuid.New(),
	}
	cmd := models.ReserveInventoryCommand{Products: validProducts}
	payload, err := sonic.Marshal(cmd)
	require.NoError(t, err)

	err = h.HandleCommand(context.Background(), &kgo.Record{
		Headers: []kgo.RecordHeader{makeMetadataHeader(t, meta)},
		Value:   payload,
	})

	require.NoError(t, err)
	assert.True(t, repo.reserveCalled, "service should run for a new command")
	assert.True(t, idempotency.createCalled, "key should be stored after success")
	assert.Equal(t, eventID.String(), idempotency.createdKey)
}
