package listeners

import (
	"context"

	"github.com/opentracing/opentracing-go"

	"github.com/customeros/mailstack/dto"
	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/logger"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/services/events"
)

type SendEmailListener struct {
	events.BaseEventListener
	repositories *repository.Repositories
	emailService interfaces.EmailService
}

func NewSendEmailListener(
	logger logger.Logger, repos *repository.Repositories, emailService interfaces.EmailService,
) interfaces.EventListener {
	return &SendEmailListener{
		BaseEventListener: events.NewBaseEventListener(
			logger,
			events.GetEventType[dto.SendEmail](), // subscribed event
			events.QueueSendEmail,                // listening on Direct queue
		),
		repositories: repos,
		emailService: emailService,
	}
}

func (l *SendEmailListener) Handle(ctx context.Context, baseEvent any) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "FlowParticipantScheduleListener.Handle")
	defer span.Finish()
	tracing.SetDefaultListenerSpanTags(ctx, span)
	tracing.LogObjectAsJson(span, "event", baseEvent)

	// First validate and extract the base event
	validatedEvent, err := l.ValidateBaseEvent(ctx, baseEvent)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	// Option 1: If you're using SendEmail DTO
	sendEmail, err := events.DecodeEventData[dto.SendEmail](ctx, validatedEvent)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}
	email := sendEmail.Email

	// get mailbox for email
	mailbox, err := l.repositories.MailboxRepository.GetMailbox(ctx, email.MailboxID)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	// get attachments if there ane any
	var attachments []*models.EmailAttachment
	if email.HasAttachment {
		attachments, err = l.repositories.EmailAttachmentRepository.ListByEmail(ctx, email.ID)
		if err != nil {
			tracing.TraceErr(span, err)
			return err
		}
	}

	switch mailbox.Provider {
	case enum.EmailGoogleWorkspace:
		// TODO
		return nil
	case enum.EmailOutlook:
		// TODO
		return nil
	default:
		return l.emailService.SendWithSMTP(ctx, mailbox, email, attachments)
	}
}
