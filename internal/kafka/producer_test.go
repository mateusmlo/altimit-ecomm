package kafka

import (
	"testing"
	"time"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/mateusmlo/altimit-ecomm/internal/errs"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

func makeMetadataHeader(t *testing.T, m models.RecordMetadata) kgo.RecordHeader {
	t.Helper()
	b, err := sonic.Marshal(m)
	require.NoError(t, err)
	return kgo.RecordHeader{Key: "metadata", Value: b}
}

func TestExtractMetadata_Success(t *testing.T) {
	eventID := uuid.New()
	sagaID := uuid.New()
	orderID := uuid.New()
	now := time.Now().Unix()

	rm := models.RecordMetadata{
		EventType: models.EventReserveInventory,
		EventID:   eventID,
		SagaID:    sagaID,
		OrderID:   orderID,
		Timestamp: now,
	}

	header := makeMetadataHeader(t, rm)
	got, err := ExtractMetadata([]kgo.RecordHeader{header})
	require.NoError(t, err)

	assert.Equal(t, models.EventReserveInventory, got.EventType)
	assert.Equal(t, eventID, got.EventID)
	assert.Equal(t, sagaID, got.SagaID)
	assert.Equal(t, orderID, got.OrderID)
	assert.Equal(t, now, got.Timestamp)
}

func TestExtractMetadata_MissingHeader(t *testing.T) {
	_, err := ExtractMetadata([]kgo.RecordHeader{})
	assert.ErrorIs(t, err, errs.ErrMissingMetadata)
}

func TestExtractMetadata_MalformedJSON(t *testing.T) {
	headers := []kgo.RecordHeader{{Key: "metadata", Value: []byte("not-json")}}
	_, err := ExtractMetadata(headers)
	assert.ErrorIs(t, err, errs.ErrMalformedPayload)
}
