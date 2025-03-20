package email

import (
	"context"

	"github.com/opentracing/opentracing-go"

	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/services/smtp"
)

func (s *emailService) SendWithSMTP(ctx context.Context, mailbox *models.Mailbox, email *models.Email, attachments []*models.EmailAttachment) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailService.SendWithSMTP")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	client := smtp.NewSMTPClient(s.repositories, mailbox)

	return client.Send(ctx, email, attachments)
}
