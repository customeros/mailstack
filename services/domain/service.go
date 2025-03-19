package domain

import (
	"context"
	"fmt"

	"github.com/customeros/mailwatcher/blscan"
	"github.com/customeros/mailwatcher/domainage"
	"github.com/opentracing/opentracing-go"
	tracingLog "github.com/opentracing/opentracing-go/log"
	"github.com/pkg/errors"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"
)

type domainService struct {
	postgres   *repository.Repositories
	cloudflare interfaces.CloudflareService
	mailbox    interfaces.MailboxServiceOld
	namecheap  interfaces.NamecheapService
	opensrs    interfaces.OpenSrsService
}

func NewDomainService(postgres *repository.Repositories, cloudflare interfaces.CloudflareService, namecheap interfaces.NamecheapService, mailbox interfaces.MailboxServiceOld, opensrs interfaces.OpenSrsService) interfaces.DomainService {
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
	err = s.postgres.DomainRepository.MarkConfigured(ctx, tenant, domain)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "Error setting domain as configured"))
	}

	return nil
}

func (s *domainService) GetDomain(ctx context.Context, domain string) (*models.MailStackDomain, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "DomainService.GetDomain")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)
	span.LogKV("request.domain", domain)

	tenant := utils.GetTenantFromContext(ctx)

	domainModel, err := s.postgres.DomainRepository.GetDomain(ctx, tenant, domain)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "Error getting domain"))
		return nil, err
	}

	return domainModel, nil
}

func (s *domainService) CheckMailstackDomainReputations(ctx context.Context) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "DomainService.CheckMailstackDomainReputations")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	// get all active domains cross tenant
	mailStackDomains, err := s.postgres.DomainRepository.GetAllActiveDomainsCrossTenant(ctx)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	if len(mailStackDomains) == 0 {
		span.LogKV("result.message", "No active domains found")
		return nil
	}

	for _, mailStackDomain := range mailStackDomains {
		domain := mailStackDomain.Domain
		tenant := mailStackDomain.Tenant

		// check reputation of domain
		_, err := s.reputationScore(ctx, domain, tenant)
		if err != nil {
			tracing.TraceErr(span, err)
			return err
		}
	}
	return nil
}

func (s *domainService) reputationScore(ctx context.Context, domain, tenant string) (int, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "DomainService.ReputationScore")
	defer span.Finish()
	tracing.TagTenant(span, tenant)
	span.LogKV("domain", domain)

	domainAgePenalty := s.domainAgePenalty(span, domain)
	blacklistPenaltyPct := s.blacklistPenaltyPercent(domain)

	score := (100 - domainAgePenalty) * (1 - (blacklistPenaltyPct)/100)

	// todo, add:
	// 7 day lookback on bounces
	// 7 day lookback on dmarc and spf

	dbEntity := models.MailstackReputation{
		CreatedAt:           utils.Now(),
		Tenant:              tenant,
		Domain:              domain,
		DomainAgePenalty:    domainAgePenalty,
		BlacklistPenaltyPct: blacklistPenaltyPct,
	}

	err := s.postgres.DomainRepository.CreateMailstackReputationScore(ctx, tenant, &dbEntity)

	return score, err
}

func (s *domainService) domainAgePenalty(span opentracing.Span, domain string) int {
	domainDates, err := domainage.GetDomainDates(domain)
	if err != nil {
		tracing.TraceErr(span, fmt.Errorf("Cannot determine domain dates: %v", err))
		return 0
	}

	if !domainDates.Success {
		return 0
	}

	domainAgeInDays := domainDates.CreationAge

	switch {
	case domainAgeInDays <= 1:
		return 75
	case domainAgeInDays <= 7:
		return 60
	case domainAgeInDays <= 10:
		return 50
	case domainAgeInDays <= 15:
		return 40
	case domainAgeInDays <= 30:
		return 30
	case domainAgeInDays <= 90:
		return 15
	default:
		return 0
	}
}

func (s *domainService) blacklistPenaltyPercent(domain string) int {
	blacklists := blscan.ScanBlacklists(domain, "domain")

	pct := (blacklists.MajorLists * 80) + (blacklists.MinorLists * 10) + (blacklists.SpamTrapLists * 20)

	if pct > 100 {
		return 100
	} else {
		return pct
	}
}

func (s *domainService) GetTenantForMailstackDomain(ctx context.Context, domain string) (string, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "DomainService.GetTenantForMailstackDomain")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)
	span.LogKV("domain", domain)

	mailStackDomainEntity, err := s.postgres.DomainRepository.GetDomainCrossTenant(ctx, domain)
	if err != nil {
		tracing.TraceErr(span, err)
		return "", err
	}
	if mailStackDomainEntity == nil {
		span.LogFields(tracingLog.Bool("result.found", false))
		return "", nil
	}

	span.LogFields(tracingLog.String("result.tenant", mailStackDomainEntity.Tenant))
	return mailStackDomainEntity.Tenant, nil
}
