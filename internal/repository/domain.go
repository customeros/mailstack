package repository

import (
	"context"

	"github.com/customeros/mailstack/internal/telemetry"
	"github.com/customeros/mailstack/internal/utils"
	"github.com/pkg/errors"

	"gorm.io/gorm"

	"github.com/customeros/mailstack/internal/models"
)

type DomainRepository interface {
	RegisterDomain(ctx context.Context, tenant, domain string) (*models.MailStackDomain, error)
	CheckDomainOwnership(ctx context.Context, tenant, domain string) (bool, error)
	GetDomain(ctx context.Context, tenant, domain string) (*models.MailStackDomain, error)
	GetActiveDomains(ctx context.Context, tenant string) ([]models.MailStackDomain, error)
	MarkConfigured(ctx context.Context, tenant, domain string) error
	SetDkimKeys(ctx context.Context, tenant, domain, dkimPublic, dkimPrivate string) error
	CreateDMARCReport(ctx context.Context, tenant string, report *models.DMARCMonitoring) error
	CreateMailstackReputationScore(ctx context.Context, tenant string, score *models.MailstackReputation) error
	GetDomainCrossTenant(ctx context.Context, domain string) (*models.MailStackDomain, error)
	GetAllActiveDomainsCrossTenant(ctx context.Context) ([]models.MailStackDomain, error)
}

type domainRepository struct {
	db *gorm.DB
}

func NewDomainRepository(db *gorm.DB) DomainRepository {
	return &domainRepository{
		db: db,
	}
}

func (r *domainRepository) CreateMailstackReputationScore(ctx context.Context, tenant string, score *models.MailstackReputation) error {
	spans, _ := telemetry.StartPostgresSpan(ctx, "DomainRepository.CreateMailstackReputationScore")
	defer spans.Finish()

	err := r.db.Create(&score).Error
	if err != nil {
		spans.TraceError(errors.Wrap(err, "db error"))
		return err
	}
	return nil
}

func (r *domainRepository) CreateDMARCReport(ctx context.Context, tenant string, report *models.DMARCMonitoring) error {
	spans, _ := telemetry.StartPostgresSpan(ctx, "DomainRepository.CreateDMARCReport")
	defer spans.Finish()

	now := utils.Now()
	report.CreatedAt = now

	err := r.db.Create(&report).Error
	if err != nil {
		spans.TraceError(errors.Wrap(err, "db error"))
		return err
	}
	return nil
}

func (r *domainRepository) RegisterDomain(ctx context.Context, tenant, domain string) (*models.MailStackDomain, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "DomainRepository.RegisterDomain")
	defer spans.Finish()

	var exists bool
	err := r.db.WithContext(ctx).
		Model(&models.MailStackDomain{}).
		Select("count(*) > 0").
		Where("tenant = ? AND domain = ?", tenant, domain).
		Find(&exists).Error
	if err != nil {
		spans.TraceError(errors.Wrap(err, "db error checking existing domain"))
		return nil, err
	}
	if exists {
		return nil, errors.New("domain already exists")
	}

	now := utils.Now()
	mailStackDomain := models.MailStackDomain{
		Tenant:    tenant,
		Domain:    domain,
		CreatedAt: now,
		UpdatedAt: now,
		Active:    true,
	}

	err = r.db.WithContext(ctx).Create(&mailStackDomain).Error
	if err != nil {
		spans.TraceError(errors.Wrap(err, "db error"))
		return nil, err
	}

	return &mailStackDomain, nil
}

func (r *domainRepository) CheckDomainOwnership(ctx context.Context, tenant, domain string) (bool, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "DomainRepository.CheckDomainOwnership")
	defer spans.Finish()
	spans.LogKV("domain", domain)

	var mailStackDomain models.MailStackDomain
	err := r.db.WithContext(ctx).
		Where("tenant = ? AND domain = ? AND active = ?", tenant, domain, true).
		First(&mailStackDomain).Error
	if err != nil {
		// If the record is not found, return false without an error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			spans.LogKV("response.exists", false)
			return false, nil
		}
		// If any other error occurs, log and trace it
		spans.TraceError(errors.Wrap(err, "db error"))
		return false, err
	}

	// If the record is found, return true
	spans.LogKV("response.exists", true)
	return true, nil
}

func (r *domainRepository) GetActiveDomains(ctx context.Context, tenant string) ([]models.MailStackDomain, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "DomainRepository.GetActiveDomains")
	defer spans.Finish()

	var mailStackDomains []models.MailStackDomain
	err := r.db.WithContext(ctx).
		Where("tenant = ? AND active = ?", tenant, true).
		Order("created_at DESC").
		Find(&mailStackDomains).Error
	if err != nil {
		spans.TraceError(errors.Wrap(err, "db error"))
		return nil, err
	}

	spans.LogKV("response.count", len(mailStackDomains))
	return mailStackDomains, nil
}

func (r *domainRepository) MarkConfigured(ctx context.Context, tenant, domain string) error {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "DomainRepository.MarkConfigured")
	defer spans.Finish()
	spans.LogKV("domain", domain)

	err := r.db.WithContext(ctx).
		Model(&models.MailStackDomain{}).
		Where("tenant = ? AND domain = ?", tenant, domain).
		UpdateColumn("configured", true).
		UpdateColumn("updated_at", utils.Now()).
		Error
	if err != nil {
		spans.TraceError(errors.Wrap(err, "db error"))
		return err
	}

	return nil
}

func (r *domainRepository) SetDkimKeys(ctx context.Context, tenant, domain, dkimPublic, dkimPrivate string) error {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "DomainRepository.SetDkimKeys")
	defer spans.Finish()
	spans.LogKV("domain", domain)

	err := r.db.WithContext(ctx).
		Model(&models.MailStackDomain{}).
		Where("tenant = ? AND domain = ?", tenant, domain).
		UpdateColumn("dkim_public", dkimPublic).
		UpdateColumn("dkim_private", dkimPrivate).
		UpdateColumn("updated_at", utils.Now()).
		Error
	if err != nil {
		spans.TraceError(errors.Wrap(err, "db error"))
		return err
	}

	return nil
}

func (r *domainRepository) GetDomain(ctx context.Context, tenant, domain string) (*models.MailStackDomain, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "DomainRepository.GetDomain")
	defer spans.Finish()
	spans.LogKV("domain", domain)

	var mailStackDomain models.MailStackDomain
	err := r.db.WithContext(ctx).
		Where("tenant = ? AND domain = ?", tenant, domain).
		First(&mailStackDomain).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			spans.LogKV("response.found", false)
			return nil, nil
		}
		spans.TraceError(errors.Wrap(err, "db error"))
		return nil, err
	}

	spans.LogKV("response.found", true)
	return &mailStackDomain, nil
}

func (r *domainRepository) GetDomainCrossTenant(ctx context.Context, domain string) (*models.MailStackDomain, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "DomainRepository.GetDomainCrossTenant")
	defer spans.Finish()
	spans.LogKV("domain", domain)

	var mailStackDomain models.MailStackDomain
	err := r.db.WithContext(ctx).
		Where("domain = ?", domain).
		First(&mailStackDomain).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		spans.TraceError(errors.Wrap(err, "db error"))
		return nil, err
	}

	return &mailStackDomain, nil
}

func (r *domainRepository) GetAllActiveDomainsCrossTenant(ctx context.Context) ([]models.MailStackDomain, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "DomainRepository.GetAllActiveDomainsCrossTenant")
	defer spans.Finish()

	var mailStackDomains []models.MailStackDomain
	err := r.db.WithContext(ctx).
		Where("active = ?", true).
		Where("configured = ?", true).
		Find(&mailStackDomains).Error
	if err != nil {
		spans.TraceError(errors.Wrap(err, "db error"))
		return nil, err
	}

	spans.LogKV("result.count", len(mailStackDomains))
	return mailStackDomains, nil
}
