package kafka

import (
	"context"

	"github.com/mateusmlo/altimit-ecomm/internal/models"
)

// EventPublisher abstracts event publishing for testability.
// *Producer implements this interface.
type EventPublisher interface {
	PublishEvent(ctx context.Context, topic string, ev models.Event) error
}
