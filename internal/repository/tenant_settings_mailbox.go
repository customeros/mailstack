package repository

import (
	"context"

	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"
	"github.com/opentracing/opentracing-go"
	tracingLog "github.com/opentracing/opentracing-go/log"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

type tenantSettingsMailboxRepository struct {
	gormDb *gorm.DB
}

type TenantSettingsMailboxRepository interface {
	GetForRampUp(ctx context.Context) ([]*models.TenantSettingsMailbox, error)
	GetById(ctx context.Context, id string) (*models.TenantSettingsMailbox, error)
	GetByMailbox(ctx context.Context, mailbox string) (*models.TenantSettingsMailbox, error)
	GetAllWithFilters(ctx context.Context, domain, userId string) ([]*models.TenantSettingsMailbox, error)

	Create(ctx context.Context, tx *gorm.DB, mailbox *models.TenantSettingsMailbox) error
	Update(ctx context.Context, tx *gorm.DB, mailbox *models.TenantSettingsMailbox) error
	UpdateStatus(ctx context.Context, id string, status models.MailboxStatus) error
	UpdateRampUpFields(ctx context.Context, mailbox *models.TenantSettingsMailbox) error
}

func NewTenantSettingsMailboxRepository(db *gorm.DB) TenantSettingsMailboxRepository {
	return &tenantSettingsMailboxRepository{gormDb: db}
}

func (r *tenantSettingsMailboxRepository) GetForRampUp(ctx context.Context) ([]*models.TenantSettingsMailbox, error) {
	span, _ := opentracing.StartSpanFromContext(ctx, "TenantSettingsMailboxRepository.GetForRampUp")
	defer span.Finish()
	tracing.SetDefaultPostgresRepositorySpanTags(ctx, span)

	var result []*models.TenantSettingsMailbox
	err := r.gormDb.
		Where("ramp_up_current < ramp_up_max and (last_ramp_up_at is null or last_ramp_up_at < ?)", utils.StartOfDayInUTC(utils.Now())).
		Find(&result).
		Error

	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}

	span.LogFields(tracingLog.Int("result.count", len(result)))

	return result, nil
}

func (r *tenantSettingsMailboxRepository) GetById(ctx context.Context, id string) (*models.TenantSettingsMailbox, error) {
	span, _ := opentracing.StartSpanFromContext(ctx, "TenantSettingsMailboxRepository.GetById")
	defer span.Finish()
	tracing.SetDefaultPostgresRepositorySpanTags(ctx, span)

	tenant := utils.GetTenantFromContext(ctx)

	span.LogFields(tracingLog.String("id", id))

	var result models.TenantSettingsMailbox
	err := r.gormDb.
		Where("tenant = ? and id = ?", tenant, id).
		First(&result).
		Error

	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		tracing.TraceErr(span, err)
		return nil, err
	}

	if err != nil && errors.Is(err, gorm.ErrRecordNotFound) {
		span.LogFields(tracingLog.Bool("result.found", false))
		return nil, nil
	}

	span.LogFields(tracingLog.Bool("result.found", true))

	return &result, nil
}

func (r *tenantSettingsMailboxRepository) GetByMailbox(ctx context.Context, mailbox string) (*models.TenantSettingsMailbox, error) {
	span, _ := opentracing.StartSpanFromContext(ctx, "TenantSettingsMailboxRepository.GetByMailbox")
	defer span.Finish()
	tracing.SetDefaultPostgresRepositorySpanTags(ctx, span)

	tenant := utils.GetTenantFromContext(ctx)

	span.LogFields(tracingLog.String("mailbox", mailbox))

	var result models.TenantSettingsMailbox
	err := r.gormDb.
		Where("tenant = ? and mailbox_username = ?", tenant, mailbox).
		First(&result).
		Error

	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		tracing.TraceErr(span, err)
		return nil, err
	}

	if err != nil && errors.Is(err, gorm.ErrRecordNotFound) {
		span.LogFields(tracingLog.Bool("result.found", false))
		return nil, nil
	}

	span.LogFields(tracingLog.Bool("result.found", true))
	return &result, nil
}

func (r *tenantSettingsMailboxRepository) GetAllWithFilters(ctx context.Context, domain, userId string) ([]*models.TenantSettingsMailbox, error) {
	span, _ := opentracing.StartSpanFromContext(ctx, "TenantSettingsMailboxRepository.GetAllWithFilters")
	defer span.Finish()
	tracing.SetDefaultPostgresRepositorySpanTags(ctx, span)
	span.LogFields(tracingLog.String("domain", domain), tracingLog.String("userId", userId))

	tenant := utils.GetTenantFromContext(ctx)

	// Start with base query for tenant
	query := r.gormDb.WithContext(ctx).Where("tenant = ?", tenant)

	// Add optional filters
	if domain != "" {
		query = query.Where("domain = ?", domain)
	}
	if userId != "" {
		query = query.Where("user_id = ?", userId)
	}

	var result []*models.TenantSettingsMailbox
	err := query.Find(&result).Error
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}

	span.LogFields(tracingLog.Int("result.count", len(result)))
	return result, nil
}

func (r *tenantSettingsMailboxRepository) Create(ctx context.Context, tx *gorm.DB, input *models.TenantSettingsMailbox) error {
	span, _ := opentracing.StartSpanFromContext(ctx, "TenantSettingsMailboxRepository.Create")
	defer span.Finish()
	tracing.SetDefaultPostgresRepositorySpanTags(ctx, span)
	tracing.LogObjectAsJson(span, "mailbox", input)

	tenant := utils.GetTenantFromContext(ctx)

	// Check if the mailbox already exists
	var exists bool
	err := r.gormDb.
		Model(&models.TenantSettingsMailbox{}).
		Select("count(*) > 0").
		Where("tenant = ? AND mailbox_username = ?", tenant, input.MailboxUsername).
		Find(&exists).Error

	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	if exists {
		return errors.New("mailbox already exists")
	}

	// Create new mailbox
	mailbox := models.TenantSettingsMailbox{
		Tenant:                  tenant,
		MailboxUsername:         input.MailboxUsername,
		MailboxPassword:         input.MailboxPassword,
		Status:                  input.Status,
		ForwardingTo:            input.ForwardingTo,
		WebmailEnabled:          input.WebmailEnabled,
		Username:                input.Username,
		UserId:                  input.UserId,
		Domain:                  input.Domain,
		LastRampUpAt:            utils.Now(),
		RampUpRate:              3,
		RampUpMax:               40,
		RampUpCurrent:           3,
		MinMinutesBetweenEmails: input.MinMinutesBetweenEmails,
		MaxMinutesBetweenEmails: input.MaxMinutesBetweenEmails,
	}

	if tx == nil {
		tx = r.gormDb
	}

	err = tx.Create(&mailbox).Error
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	return nil
}

func (r *tenantSettingsMailboxRepository) Update(ctx context.Context, tx *gorm.DB, input *models.TenantSettingsMailbox) error {
	span, _ := opentracing.StartSpanFromContext(ctx, "TenantSettingsMailboxRepository.Update")
	defer span.Finish()
	tracing.SetDefaultPostgresRepositorySpanTags(ctx, span)
	tracing.LogObjectAsJson(span, "mailbox", input)

	tenant := utils.GetTenantFromContext(ctx)

	// Get existing mailbox to verify it exists and get its ID
	var existing models.TenantSettingsMailbox
	err := r.gormDb.
		Where("tenant = ? AND mailbox_username = ?", tenant, input.MailboxUsername).
		First(&existing).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("mailbox not found")
		}
		tracing.TraceErr(span, err)
		return err
	}

	// Preserve ID and tenant
	input.ID = existing.ID
	input.Tenant = tenant
	input.UpdatedAt = utils.Now()

	if tx == nil {
		tx = r.gormDb
	}

	err = tx.Save(input).Error
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	return nil
}

func (r *tenantSettingsMailboxRepository) UpdateStatus(ctx context.Context, id string, status models.MailboxStatus) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "TenantSettingsMailboxRepository.UpdateStatus")
	defer span.Finish()
	tracing.SetDefaultPostgresRepositorySpanTags(ctx, span)
	tracing.TagEntity(span, id)
	span.LogKV("status", status)

	tenant := utils.GetTenantFromContext(ctx)

	err := r.gormDb.WithContext(ctx).
		Model(&models.TenantSettingsMailbox{}).
		Where("tenant = ? AND id = ?", tenant, id).
		UpdateColumns(map[string]interface{}{
			"status":     status,
			"updated_at": utils.Now(),
		}).Error
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "db error"))
		return err
	}

	return nil
}

func (r *tenantSettingsMailboxRepository) UpdateRampUpFields(ctx context.Context, mailbox *models.TenantSettingsMailbox) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "TenantSettingsMailboxRepository.UpdateRampUpFields")
	defer span.Finish()
	tracing.SetDefaultPostgresRepositorySpanTags(ctx, span)
	tracing.TagEntity(span, mailbox.ID)
	span.LogFields(tracingLog.Object("ramp_up_current", mailbox.RampUpCurrent))
	span.LogFields(tracingLog.Object("last_ramp_up_at", mailbox.LastRampUpAt))

	err := r.gormDb.WithContext(ctx).
		Model(&models.TenantSettingsMailbox{}).
		Where("id = ?", mailbox.ID).
		Updates(map[string]interface{}{
			"ramp_up_current": mailbox.RampUpCurrent,
			"last_ramp_up_at": mailbox.LastRampUpAt,
		}).Error

	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	return nil
}
