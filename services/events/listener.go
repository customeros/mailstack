package events

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"

	"github.com/customeros/mailstack/dto"
	mailstack_errors "github.com/customeros/mailstack/errors"
	"github.com/customeros/mailstack/internal/logger"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"
)

// EventListener interface defines what all listeners must implement
type EventListener interface {
	Handle(ctx context.Context, baseEvent any) error
	GetEventType() string
	GetQueueName() string
}

// BaseEventListener provides common functionality for all listeners
type BaseEventListener struct {
	logger    logger.Logger
	eventType string
	queueName string
}

// NewBaseEventListener creates a new base event listener
func NewBaseEventListener(logger logger.Logger, eventType, queueName string) BaseEventListener {
	return BaseEventListener{
		logger:    logger,
		eventType: eventType,
		queueName: queueName,
	}
}

func (b BaseEventListener) GetEventType() string {
	return b.eventType
}

func (b BaseEventListener) GetQueueName() string {
	return b.queueName
}

func (b BaseEventListener) ValidateBaseEvent(ctx context.Context, input any) (*dto.Event, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "Events.ValidateEvent")
	defer span.Finish()
	tracing.SetDefaultListenerSpanTags(ctx, span)

	tenant := utils.GetTenantFromContext(ctx)
	if tenant == "" {
		err := mailstack_errors.ErrTenantNotSet
		tracing.TraceErr(span, err)
		return nil, err
	}

	message, ok := input.(dto.Event)
	if !ok {
		err := errors.New("unable to cast to event type")
		tracing.TraceErr(span, err)
		return nil, err
	}

	if message.Event.Data == nil {
		err := errors.New("message data is nil")
		tracing.TraceErr(span, err)
		return nil, err
	}

	if message.Event.EntityId == "" {
		err := errors.New("entity id is empty")
		tracing.TraceErr(span, err)
		return nil, err
	}

	if message.Event.Tenant == "" {
		err := errors.New("tenant is empty")
		tracing.TraceErr(span, err)
		return nil, err
	}

	if message.Event.EventType == "" {
		err := errors.New("event type is empty")
		tracing.TraceErr(span, err)
		return nil, err
	}

	return &message, nil
}

func DecodeEventData[T any](ctx context.Context, event *dto.Event) (T, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "Listener.DecodeEventData")
	defer span.Finish()
	tracing.SetDefaultListenerSpanTags(ctx, span)

	var decoded T

	bytes, ok := event.Event.Data.(map[string]interface{})
	if !ok {
		err := errors.New("failed to cast event data to map[string]interface{}")
		tracing.TraceErr(span, err)
		return decoded, err
	}

	// Convert map[string]interface{} to JSON bytes
	jsonBytes, err := json.Marshal(bytes)
	if err != nil {
		tracing.TraceErr(span, err)
		return decoded, err
	}

	// Now unmarshal the JSON bytes into the target struct or map
	err = json.Unmarshal(jsonBytes, &decoded)
	if err != nil {
		tracing.TraceErr(span, err)
		return decoded, err
	}

	return decoded, nil
}

func GetEventType[T any]() string {
	var t T
	eventType := reflect.TypeOf(t)
	if eventType.Kind() == reflect.Ptr {
		eventType = eventType.Elem()
	}
	return eventType.Name()
}
