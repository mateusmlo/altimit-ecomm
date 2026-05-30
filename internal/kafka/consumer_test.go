package kafka

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mateusmlo/altimit-ecomm/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
)

const consumerTestSource = "source.commands"

// newConsumerCluster creates an isolated fake cluster pre-seeded with source and DLQ topics.
func newConsumerCluster(t *testing.T) []string {
	t.Helper()
	fk, err := kfake.NewCluster(kfake.SeedTopics(1, consumerTestSource, dlqTestTopic))
	require.NoError(t, err)
	t.Cleanup(fk.Close)
	return fk.ListenAddrs()
}

func consumerTestCfg(brokers []string) *config.Config {
	cfg := &config.Config{}
	cfg.Kafka.Brokers = brokers
	return cfg
}

// produce writes records to the source topic and blocks until kfake acknowledges them.
func produce(t *testing.T, brokers []string, values ...string) {
	t.Helper()
	cl, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	require.NoError(t, err)
	defer cl.Close()
	for i, v := range values {
		require.NoError(t, cl.ProduceSync(context.Background(), &kgo.Record{
			Topic: consumerTestSource,
			Key:   []byte(fmt.Sprintf("key-%d", i)),
			Value: []byte(v),
		}).FirstErr())
	}
}

// drainDLQ reads up to want records from the DLQ topic, waiting up to 5 s.
func drainDLQ(t *testing.T, brokers []string, want int) []*kgo.Record {
	t.Helper()
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ConsumeTopics(dlqTestTopic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	require.NoError(t, err)
	defer cl.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var records []*kgo.Record
	for len(records) < want && ctx.Err() == nil {
		fetches := cl.PollFetches(ctx)
		fetches.EachRecord(func(r *kgo.Record) {
			records = append(records, r)
		})
	}
	return records
}

// headerVal returns the value of the first matching header, or "".
func headerVal(headers []kgo.RecordHeader, key string) string {
	for _, h := range headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

// TestConsume_HandlerError_PublishesToDLQAndContinues verifies that when every
// record fails, the consumer publishes each one to the DLQ and keeps running
// instead of stopping at the first error.
func TestConsume_HandlerError_PublishesToDLQAndContinues(t *testing.T) {
	brokers := newConsumerCluster(t)
	produce(t, brokers, "msg-0", "msg-1")

	dlqClient := newClient(t, brokers)
	consumer, err := NewConsumer(
		consumerTestCfg(brokers), "test-group-all-fail",
		[]string{consumerTestSource}, dlqTestTopic, dlqClient,
	)
	require.NoError(t, err)
	t.Cleanup(consumer.Client.Close)

	handlerErr := errors.New("processing failed")
	processed := make(chan string, 10)
	handler := func(ctx context.Context, r *kgo.Record) error {
		processed <- string(r.Value)
		return handlerErr
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	go consumer.Consume(ctx, handler) //nolint:errcheck

	// Block until both records are handled — if the consumer stops on first
	// error (regression), this select will time out on the second receive.
	var msgs []string
	for len(msgs) < 2 {
		select {
		case msg := <-processed:
			msgs = append(msgs, msg)
		case <-ctx.Done():
			t.Fatalf("consumer stopped early: only %d/2 records processed", len(msgs))
		}
	}

	// Both failed records must arrive in the DLQ.
	dlqRecords := drainDLQ(t, brokers, 2)
	require.Len(t, dlqRecords, 2, "expected 2 DLQ records, got %d", len(dlqRecords))
	for _, r := range dlqRecords {
		assert.Equal(t, handlerErr.Error(), headerVal(r.Headers, "dlq-error"))
	}
}

// TestConsume_MixedResults_OnlyFailedRecordsGoToDLQ verifies that records
// processed successfully are not forwarded to the DLQ, and that a failure on
// one record does not prevent the others from being processed.
func TestConsume_MixedResults_OnlyFailedRecordsGoToDLQ(t *testing.T) {
	brokers := newConsumerCluster(t)
	// Produce three records; the handler will fail for "fail" values.
	produce(t, brokers, "fail", "pass", "fail")

	dlqClient := newClient(t, brokers)
	consumer, err := NewConsumer(
		consumerTestCfg(brokers), "test-group-mixed",
		[]string{consumerTestSource}, dlqTestTopic, dlqClient,
	)
	require.NoError(t, err)
	t.Cleanup(consumer.Client.Close)

	handlerErr := errors.New("bad record")
	var mu sync.Mutex
	var processed []string
	handler := func(ctx context.Context, r *kgo.Record) error {
		mu.Lock()
		processed = append(processed, string(r.Value))
		mu.Unlock()
		if string(r.Value) == "fail" {
			return handlerErr
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	go consumer.Consume(ctx, handler) //nolint:errcheck

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(processed) >= 3
	}, 12*time.Second, 50*time.Millisecond, "timed out waiting for all 3 records to be processed")

	// Exactly 2 records (the "fail" ones) should appear in the DLQ.
	dlqRecords := drainDLQ(t, brokers, 2)
	require.Len(t, dlqRecords, 2, "expected 2 DLQ records, got %d", len(dlqRecords))
	for _, r := range dlqRecords {
		assert.Equal(t, "fail", string(r.Value), "only failed records should be in DLQ")
		assert.Equal(t, handlerErr.Error(), headerVal(r.Headers, "dlq-error"))
	}
}
