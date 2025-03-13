package interfaces

import "context"

type DomainService interface {
	ConfigureDomain(ctx context.Context, domain, redirectWebsite string) error
}
