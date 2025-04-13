package interfaces

import (
	"context"
	"time"

	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
)

type MailboxRepository interface {
	GetMailboxes(ctx context.Context) ([]*models.Mailbox, error)
	GetMailboxesByUserID(ctx context.Context, userID string) ([]*models.Mailbox, error)
	GetMailbox(ctx context.Context, id string) (*models.Mailbox, error)
	GetMailboxByEmailAddress(ctx context.Context, emailAddress string) (*models.Mailbox, error)
	GetMailboxByEmailAddressCrossTenant(ctx context.Context, emailAddress string) (*models.Mailbox, error)
	GetForConfiguration(ctx context.Context, limit int) ([]*models.Mailbox, error)
	GetAllWithFilters(ctx context.Context, provider enum.EmailProvider, domain, userId string) ([]*models.Mailbox, error)
	SaveMailbox(ctx context.Context, mailbox models.Mailbox) (string, error)
	DeleteMailbox(ctx context.Context, id string) error
	UpdateConnectionStatus(ctx context.Context, mailboxID string, status enum.ConnectionStatus, errorMessage string) error

	UpdateProvisionStatus(ctx context.Context, id string, status models.MailboxProvisionStatus) error
	ConfigureAttempt(ctx context.Context, id string) error
	GetForRampUp(ctx context.Context) ([]*models.Mailbox, error)
	UpdateRampUpFields(ctx context.Context, mailbox *models.Mailbox) error
	UpdateOauthToken(ctx context.Context, mailboxID, accessToken, refreshToken string, tokenExpiry *time.Time) error
	MarkForManualRefresh(ctx context.Context, mailboxID string, needsManualRefresh bool) error
}
