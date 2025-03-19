package interfaces

import (
	"context"

	"gorm.io/gorm"

	"github.com/customeros/mailstack/internal/models"
)

type MailboxServiceOld interface {
	CreateMailbox(ctx context.Context, tx *gorm.DB, request CreateMailboxRequest) error
	IsDomainAvailable(ctx context.Context, domain string) (ok, available bool)
	RecommendOutboundDomains(ctx context.Context, domainRoot string, count int) []string
	ReputationScore(ctx context.Context, domain, tenant string) (int, error)
	GetMailboxes(ctx context.Context, domain, userId string) ([]*models.TenantSettingsMailbox, error)
	GetByMailbox(ctx context.Context, username, domain string) (*models.TenantSettingsMailbox, error)
	RampUpMailboxes(ctx context.Context) error
	ConfigureMailbox(ctx context.Context, mailboxId string) error
}

type CreateMailboxRequest struct {
	Domain                string
	Username              string
	Password              string
	UserId                string
	WebmailEnabled        bool
	ForwardingTo          []string
	IgnoreDomainOwnership bool
}
