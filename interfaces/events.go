package interfaces

import (
	"context"

	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/utils"
)

type EventPublisher interface {
	PublishFanoutEvent(ctx context.Context, entityId string, entityType enum.EntityType, message interface{}) error
	PublishDirectEvent(ctx context.Context, entityId string, entityType enum.EntityType, message interface{}) error
	PublishNotification(ctx context.Context, tenant string, entityId string, entityType enum.EntityType, details *utils.EventCompletedDetails)
	PublishNotificationBulk(ctx context.Context, tenant string, entityIds []string, entityType enum.EntityType, details *utils.EventCompletedDetails)
	Close() error
}

type EventListener interface {
	Handle(ctx context.Context, event any) error
	GetEventType() string
	GetQueueName() string
}

type EventSubscriber interface {
	RegisterListener(listener EventListener)
	ListenQueue(queueName string) error
	ListenQueueExclusive(queueName string) error
	Close() error
}
