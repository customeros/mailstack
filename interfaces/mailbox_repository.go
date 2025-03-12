package interfaces

import (
	"context"

	"github.com/customeros/mailstack/internal/models"
)

type MailboxRepository interface {
	GetMailboxes(ctx context.Context) ([]*models.Mailbox, error)
	GetMailbox(ctx context.Context, id string) (*models.Mailbox, error)
	GetMailboxByEmailAddress(ctx context.Context, emailAddress string) (*models.Mailbox, error)
	SaveMailbox(ctx context.Context, mailbox models.Mailbox) error
	DeleteMailbox(ctx context.Context, id string) error
}
