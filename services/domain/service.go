package domain

import (
	"context"

	"github.com/customeros/mailstack/config"
	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/repository/postgres"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
)

type domainService struct {
	postgres   *postgres.Repositories
	cloudflare interfaces.CloudflareService
	mailbox    interfaces.MailboxService
	namecheap  interfaces.NamecheapService
	opensrs    interfaces.OpenSrsService
}

func NewDomainService(postgres *postgres.Repositories, cloudflare interfaces.CloudflareService, namecheap interfaces.NamecheapService, mailbox interfaces.MailboxService, opensrs interfaces.OpenSrsService) interfaces.DomainService {
	return &domainService{
		postgres:   postgres,
		cloudflare: cloudflare,
		mailbox:    mailbox,
		namecheap:  namecheap,
		opensrs:    opensrs,
	}
}

func (s *domainService) ConfigureDomain(ctx context.Context, domain, redirectWebsite string) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "DomainService.ConfigureDomain")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)
	span.LogKV("request.domain", domain)
	span.LogKV("request.redirectWebsite", redirectWebsite)

	// validate tenant
	err := utils.ValidateTenant(ctx)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}
	tenant := utils.GetTenantFromContext(ctx)

	// setup domain in cloudflare
	nameservers, err := s.cloudflare.SetupDomainForMailStack(ctx, tenant, domain, redirectWebsite)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "Error setting up domain in Cloudflare"))
		return err
	}

	// setup domain in openSRS
	err = s.opensrs.SetupDomain(ctx, tenant, domain)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "Error setting up domain in OpenSRS"))
		return err
	}

	// replace nameservers in namecheap
	err = s.namecheap.UpdateNameservers(ctx, tenant, domain, nameservers)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "Error updating nameservers"))
		return err
	}

	// mark domain as configured
	err = s.postgres.MailStackDomainRepository.MarkConfigured(ctx, tenant, domain)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "Error setting domain as configured"))
	}

	return nil
}
