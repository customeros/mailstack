package listeners

import (
	"context"
	"errors"

	"github.com/customeros/mailstack/dto"
	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/logger"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/telemetry"
	"github.com/customeros/mailstack/services/events"
)

type ReceiveEmailListener struct {
	events.BaseEventListener
	repositories  *repository.Repositories
	imapProcessor interfaces.IMAPProcessor
}

func NewReceiveEmailListener(
	logger logger.Logger, repos *repository.Repositories, imapProcessor interfaces.IMAPProcessor,
) interfaces.EventListener {
	return &ReceiveEmailListener{
		BaseEventListener: events.NewBaseEventListener(
			logger,
			events.GetEventType[dto.EmailReceived](), // subscribed event
			events.QueueReceiveEmail,                 // listening on Direct queue
		),
		repositories:  repos,
		imapProcessor: imapProcessor,
	}
}

func (l *ReceiveEmailListener) Handle(ctx context.Context, baseEvent any) error {
	spans, ctx := telemetry.StartListenerSpan(ctx, "ReceiveEmailListener.Handle")
	defer spans.Finish()
	spans.LogObjectAsJson("event", baseEvent)

	// First validate and extract the base event
	validatedEvent, err := l.ValidateBaseEvent(ctx, baseEvent)
	if err != nil {
		spans.TraceError(err)
		return err
	}

	inboundEmail, err := events.DecodeEventData[dto.EmailReceived](ctx, validatedEvent)
	if err != nil {
		spans.TraceError(err)
		return err
	}

	switch inboundEmail.Source {
	case enum.EmailImportIMAP:
		return l.imapProcessor.ProcessIMAPMessage(ctx, inboundEmail)
	default:
		return errors.New("not implemented yet")
	}
}
