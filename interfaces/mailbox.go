package interfaces

import (
	"context"

	"github.com/customeros/mailstack/internal/models"
)

type MailboxService interface {
	EnrollMailbox(ctx context.Context, mailbox *models.Mailbox) (*models.Mailbox, error)
}
