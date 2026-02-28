package kafka

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordMetadata_MarshalBinary(t *testing.T) {
	eventID := uuid.New()
	sagaID := uuid.New()
	orderID := uuid.New()
	now := time.Now().Unix()

	rm := RecordMetadata{
		EventType: models.EventReserveInventory,
		EventID:   eventID,
		SagaID:    sagaID,
		OrderID:   orderID,
		Timestamp: now,
	}

	b, err := rm.MarshalBinary()
	require.NoError(t, err)
	assert.NotEmpty(t, b)

	// Unmarshal back and verify round-trip
	var decoded RecordMetadata
	require.NoError(t, json.Unmarshal(b, &decoded))

	assert.Equal(t, models.EventReserveInventory, decoded.EventType)
	assert.Equal(t, eventID, decoded.EventID)
	assert.Equal(t, sagaID, decoded.SagaID)
	assert.Equal(t, orderID, decoded.OrderID)
	assert.Equal(t, now, decoded.Timestamp)
}

func TestRecordMetadata_MarshalBinary_ZeroValue(t *testing.T) {
	rm := RecordMetadata{}

	b, err := rm.MarshalBinary()
	require.NoError(t, err)
	assert.NotEmpty(t, b)

	var decoded RecordMetadata
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, rm, decoded)
}
