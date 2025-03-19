package mailboxold

import (
	"context"
	"fmt"

	"github.com/customeros/mailwatcher/blscan"
	"github.com/customeros/mailwatcher/domainage"
	"github.com/opentracing/opentracing-go"

	models "github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"
)

func (s *mailboxService) ReputationScore(ctx context.Context, domain, tenant string) (int, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "MailboxService.ReputationScore")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)
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

func (s *mailboxService) domainAgePenalty(span opentracing.Span, domain string) int {
	domainDates, err := domainage.GetDomainDates(domain)
	if err != nil {
		tracing.TraceErr(span, fmt.Errorf("cannot determine domain dates: %v", err))
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

func (s *mailboxService) blacklistPenaltyPercent(domain string) int {
	blacklists := blscan.ScanBlacklists(domain, "domain")

	pct := (blacklists.MajorLists * 80) + (blacklists.MinorLists * 10) + (blacklists.SpamTrapLists * 20)

	if pct > 100 {
		return 100
	} else {
		return pct
	}
}
