package interfaces

import (
	"context"

	"github.com/customeros/mailstack/internal/models"
)

type DomainService interface {
	ConfigureDomain(ctx context.Context, domain, redirectWebsite string) error
	GetDomain(ctx context.Context, domain string) (*models.MailStackDomain, error)
}
