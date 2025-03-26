package interfaces

import (
	"context"

	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
)

type MailboxService interface {
	EnrollMailbox(ctx context.Context, mailbox *models.Mailbox) (*models.Mailbox, error)
	CreateMailbox(ctx context.Context, request CreateMailboxRequest) error
	ConfigureMailbox(ctx context.Context, mailboxId string) error
	RampUpMailboxes(ctx context.Context) error
	GetMailboxes(ctx context.Context, provider enum.EmailProvider, domain, userId string) ([]*models.Mailbox, error)
	GetMailboxByEmailAddress(ctx context.Context, emailAddress string) (*models.Mailbox, error)
}

type CreateMailboxRequest struct {
	Domain                string
	Username              string
	Password              string
	UserId                string
	WebmailEnabled        bool
	ForwardingTo          []string
	IgnoreDomainOwnership bool
	SenderID              string
}
