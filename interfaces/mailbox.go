package interfaces

import (
	"context"

	"github.com/customeros/mailstack/internal/models"
	"gorm.io/gorm"
)

type MailboxService interface {
	CreateMailbox(ctx context.Context, tx *gorm.DB, request CreateMailboxRequest) error
	IsDomainAvailable(ctx context.Context, domain string) (ok, available bool)
	RecommendOutboundDomains(ctx context.Context, domainRoot string, count int) []string
	ReputationScore(ctx context.Context, domain, tenant string) (int, error)
	GetMailboxes(ctx context.Context, domain string) ([]*models.TenantSettingsMailbox, error)
}

type CreateMailboxRequest struct {
	Domain         string
	Username       string
	Password       string
	UserId         string
	WebmailEnabled bool
	ForwardingTo   []string

	IgnoreDomainOwnership bool
}
