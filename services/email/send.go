package email

import (
	"context"

	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/telemetry"
	"github.com/customeros/mailstack/services/smtp"
)

func (s *emailService) SendWithSMTP(ctx context.Context, mailbox *models.Mailbox, email *models.EmailLog, attachments []*models.EmailAttachment) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailService.SendWithSMTP")
	defer spans.Finish()

	client := smtp.NewSMTPClient(s.repositories, mailbox)

	return client.Send(ctx, email, attachments)
}
