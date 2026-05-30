package kafka

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
)

const dlqTestTopic = "orders.dlq"

func newFakeCluster(t *testing.T) (*kfake.Cluster, []string) {
	t.Helper()
	fk, err := kfake.NewCluster(kfake.SeedTopics(1, dlqTestTopic))
	require.NoError(t, err)
	t.Cleanup(fk.Close)
	return fk, fk.ListenAddrs()
}

func newClient(t *testing.T, brokers []string, opts ...kgo.Opt) *kgo.Client {
	t.Helper()
	cl, err := kgo.NewClient(append([]kgo.Opt{kgo.SeedBrokers(brokers...)}, opts...)...)
	require.NoError(t, err)
	t.Cleanup(cl.Close)
	return cl
}

func TestPublishToDLQ_PublishesRecordWithErrorHeader(t *testing.T) {
	_, brokers := newFakeCluster(t)
	producer := newClient(t, brokers)
	consumer := newClient(t, brokers,
		kgo.ConsumeTopics(dlqTestTopic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)

	original := &kgo.Record{
		Key:   []byte("order-123"),
		Value: []byte(`{"order_id":"123"}`),
		Headers: []kgo.RecordHeader{
			{Key: "metadata", Value: []byte(`{"event_type":"reserve_inventory"}`)},
		},
	}
	handlerErr := errors.New("handler processing failed")

	err := PublishToDLQ(context.Background(), producer, dlqTestTopic, original, handlerErr)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fetches := consumer.PollFetches(ctx)
	require.NoError(t, fetches.Err())

	var received []*kgo.Record
	fetches.EachRecord(func(r *kgo.Record) {
		received = append(received, r)
	})
	require.Len(t, received, 1)

	got := received[0]
	assert.Equal(t, original.Key, got.Key)
	assert.Equal(t, original.Value, got.Value)

	headerMap := make(map[string]string)
	for _, h := range got.Headers {
		headerMap[h.Key] = string(h.Value)
	}
	assert.Equal(t, `{"event_type":"reserve_inventory"}`, headerMap["metadata"], "original headers preserved")
	assert.Equal(t, handlerErr.Error(), headerMap["dlq-error"], "error header added")
}

func TestPublishToDLQ_PreservesOriginalHeadersUnmodified(t *testing.T) {
	original := &kgo.Record{
		Key:   []byte("key"),
		Value: []byte("value"),
		Headers: []kgo.RecordHeader{
			{Key: "a", Value: []byte("1")},
			{Key: "b", Value: []byte("2")},
		},
	}

	// Verify that PublishToDLQ does not mutate the original record's headers slice.
	_, brokers := newFakeCluster(t)
	client := newClient(t, brokers)

	_ = PublishToDLQ(context.Background(), client, dlqTestTopic, original, errors.New("oops"))

	assert.Len(t, original.Headers, 2, "original record headers must not be mutated")
}
