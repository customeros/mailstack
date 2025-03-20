package interfaces

import (
	"context"

	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
)

type EmailService interface {
	ScheduleSend(ctx context.Context, email *models.Email, attachmentIDs []string) (string, enum.EmailStatus, error)

	// used only by events
	SendWithSMTP(ctx context.Context, mailbox *models.Mailbox, email *models.Email, attachments []*models.EmailAttachment) error
}
