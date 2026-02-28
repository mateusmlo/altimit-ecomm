package inventory

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/mateusmlo/altimit-ecomm/internal/config"
	"github.com/mateusmlo/altimit-ecomm/internal/errs"
	"github.com/mateusmlo/altimit-ecomm/internal/kafka"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

// newTestHandler builds a Handler with a nil producer.
// Only safe for tests that never reach publishReply.
func newTestHandler(svc *InventoryService) *Handler {
	return &Handler{
		service:  svc,
		producer: nil,
		config:   &config.Config{},
	}
}

func makeMetadataHeader(t *testing.T, m kafka.RecordMetadata) kgo.RecordHeader {
	t.Helper()
	b, err := m.MarshalBinary()
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

	meta := kafka.RecordMetadata{
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

	meta := kafka.RecordMetadata{
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

	meta := kafka.RecordMetadata{
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
