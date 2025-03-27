package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/customeros/mailstack/internal/telemetry"

	"github.com/opentracing/opentracing-go"
	tracingLog "github.com/opentracing/opentracing-go/log"
	"gorm.io/gorm"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"
)

type mailboxRepository struct {
	db *gorm.DB
}

// GetAllWithFilters implements interfaces.MailboxRepository.
func (r *mailboxRepository) GetAllWithFilters(ctx context.Context, provider enum.EmailProvider, domain string, userId string) ([]*models.Mailbox, error) {
	span, _ := opentracing.StartSpanFromContext(ctx, "mailboxRepository.GetAllWithFilters")
	defer span.Finish()
	tracing.SetDefaultPostgresRepositorySpanTags(ctx, span)
	span.LogKV("provider", provider, "domain", domain, "userId", userId)

	tenant := utils.GetTenantFromContext(ctx)

	// Start with base query for tenant
	query := r.db.WithContext(ctx).Where("tenant = ?", tenant)

	// Add optional filters
	if provider != "" {
		query = query.Where("provider = ?", provider)
	}
	if domain != "" {
		query = query.Where("domain = ?", domain)
	}
	if userId != "" {
		query = query.Where("user_id = ?", userId)
	}

	var result []*models.Mailbox
	err := query.Find(&result).Error
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}

	span.LogFields(tracingLog.Int("result.count", len(result)))
	return result, nil
}

func NewMailboxRepository(db *gorm.DB) interfaces.MailboxRepository {
	return &mailboxRepository{db: db}
}

func (r *mailboxRepository) GetMailboxes(ctx context.Context) ([]*models.Mailbox, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "mailboxRepository.GetMailboxes")
	defer span.Finish()
	tracing.SetDefaultPostgresRepositorySpanTags(ctx, span)

	var mailboxes []*models.Mailbox
	result := r.db.Find(&mailboxes)
	if result.Error != nil {
		tracing.TraceErr(span, result.Error)
		return nil, result.Error
	}
	return mailboxes, nil
}

func (r *mailboxRepository) GetMailboxesByUserID(ctx context.Context, userID string) ([]*models.Mailbox, error) {
	spans, ctx := telemetry.StartSpan(ctx, "mailboxRepository.GetMailboxesByUserID")
	defer telemetry.FinishSpans(spans)
	telemetry.TagComponentPostgres(spans)
	spans.LogKV("userId", userID)

	tenant := utils.GetTenantFromContext(ctx)
	var mailboxes []*models.Mailbox

	// search by user and tenant
	result := r.db.Where("user_id = ? AND tenant = ?", userID, tenant).Find(&mailboxes)
	if result.Error != nil {
		spans.TraceError(result.Error)
		return nil, result.Error
	}

	spans.LogKV("result.count", len(mailboxes))
	return mailboxes, nil
}

func (r *mailboxRepository) GetMailbox(ctx context.Context, id string) (*models.Mailbox, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "mailboxRepository.GetMailbox")
	defer span.Finish()
	tracing.SetDefaultPostgresRepositorySpanTags(ctx, span)
	tracing.TagEntity(span, id)

	var mailbox models.Mailbox
	err := r.db.First(&mailbox, "id = ?", id).Error
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}

	span.LogKV("result.found", true)
	return &mailbox, nil
}

func (r *mailboxRepository) GetMailboxByEmailAddress(ctx context.Context, emailAddress string) (*models.Mailbox, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "mailboxRepository.GetMailboxByEmailAddress")
	defer span.Finish()
	tracing.SetDefaultPostgresRepositorySpanTags(ctx, span)
	span.LogKV("emailAddress", emailAddress)

	tenant := utils.GetTenantFromContext(ctx)

	var mailbox models.Mailbox
	err := r.db.First(&mailbox, "tenant = ? AND email_address = ?", tenant, emailAddress).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			span.LogKV("result.found", false)
			return nil, nil
		}
		tracing.TraceErr(span, err)
		return nil, err
	}

	span.LogKV("result.found", true)
	span.LogKV("result.mailbox.id", mailbox.ID)
	return &mailbox, nil
}

func (r *mailboxRepository) GetMailboxByEmailAddressCrossTenant(ctx context.Context, emailAddress string) (*models.Mailbox, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "mailboxRepository.GetMailboxByEmailAddressCrossTenant")
	defer span.Finish()
	tracing.SetDefaultPostgresRepositorySpanTags(ctx, span)
	span.LogKV("emailAddress", emailAddress)

	var mailbox models.Mailbox
	err := r.db.First(&mailbox, "email_address = ?", emailAddress).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			span.LogKV("result.found", false)
			return nil, nil
		}
		tracing.TraceErr(span, err)
		return nil, err
	}

	span.LogKV("result.found", true)
	span.LogKV("result.mailbox.id", mailbox.ID)
	return &mailbox, nil
}

func (r *mailboxRepository) SaveMailbox(ctx context.Context, mailbox models.Mailbox) (string, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "mailboxRepository.SaveMailbox")
	defer span.Finish()
	tracing.SetDefaultPostgresRepositorySpanTags(ctx, span)

	// Perform the save operation
	result := r.db.Save(&mailbox)
	if result.Error != nil {
		return "", result.Error
	}

	return mailbox.ID, nil
}

func (r *mailboxRepository) DeleteMailbox(ctx context.Context, id string) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "mailboxRepository.DeleteMailbox")
	defer span.Finish()
	tracing.SetDefaultPostgresRepositorySpanTags(ctx, span)

	return r.db.Delete(&models.Mailbox{}, "id = ?", id).Error
}

// UpdateConnectionStatus updates the connection status and error message for a mailbox
func (r *mailboxRepository) UpdateConnectionStatus(ctx context.Context, mailboxID string, status enum.ConnectionStatus, errorMessage string) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "mailboxRepository.UpdateConnectionStatus")
	defer span.Finish()
	tracing.TagComponentPostgresRepository(span)
	span.SetTag("mailbox.id", mailboxID)
	span.SetTag("status", status)

	// Create a timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Update the connection status, error message, and last connection check time
	result := r.db.WithContext(timeoutCtx).Model(&models.Mailbox{}).
		Where("id = ?", mailboxID).
		Updates(map[string]interface{}{
			"connection_status":     status,
			"error_message":         errorMessage,
			"last_connection_check": time.Now(),
			"updated_at":            time.Now(),
		})

	if result.Error != nil {
		tracing.TraceErr(span, result.Error)
		return fmt.Errorf("failed to update mailbox connection status: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		err := fmt.Errorf("mailbox with ID %s not found", mailboxID)
		tracing.TraceErr(span, err)
		return err
	}

	span.LogKV("affectedRows", result.RowsAffected)
	return nil
}

func (r *mailboxRepository) GetForRampUp(ctx context.Context) ([]*models.Mailbox, error) {
	span, _ := opentracing.StartSpanFromContext(ctx, "mailboxRepository.GetForRampUp")
	defer span.Finish()
	tracing.SetDefaultPostgresRepositorySpanTags(ctx, span)

	var result []*models.Mailbox
	err := r.db.WithContext(ctx).
		Where("provider = ?", enum.EmailMailstack).
		Where("provision_status = ?", models.MailboxStatusProvisioned).
		Where("ramp_up_current < ramp_up_max and (last_ramp_up_at is null or last_ramp_up_at < ?)", utils.StartOfDayInUTC(utils.Now())).
		Find(&result).
		Error

	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}

	span.LogKV("result.count", len(result))

	return result, nil
}

func (r *mailboxRepository) UpdateRampUpFields(ctx context.Context, mailbox *models.Mailbox) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "mailboxRepository.UpdateRampUpFields")
	defer span.Finish()
	tracing.SetDefaultPostgresRepositorySpanTags(ctx, span)
	tracing.TagEntity(span, mailbox.ID)
	span.LogKV("ramp_up_current", mailbox.RampUpCurrent)
	span.LogFields(tracingLog.Object("last_ramp_up_at", mailbox.LastRampUpAt))

	err := r.db.WithContext(ctx).
		Model(&models.Mailbox{}).
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

func (r *mailboxRepository) ConfigureAttempt(ctx context.Context, id string) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "mailboxRepository.ConfigureAttempt")
	defer span.Finish()
	tracing.SetDefaultPostgresRepositorySpanTags(ctx, span)
	tracing.TagEntity(span, id)

	err := r.db.WithContext(ctx).
		Model(&models.Mailbox{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"configure_attempt_at": time.Now(),
		}).Error

	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	return nil
}

func (r *mailboxRepository) UpdateProvisionStatus(ctx context.Context, id string, provisionStatus models.MailboxProvisionStatus) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "mailboxRepository.UpdateProvisionStatus")
	defer span.Finish()
	tracing.SetDefaultPostgresRepositorySpanTags(ctx, span)
	tracing.TagEntity(span, id)
	span.LogKV("provisionStatus", provisionStatus)

	tenant := utils.GetTenantFromContext(ctx)

	err := r.db.WithContext(ctx).
		Model(&models.Mailbox{}).
		Where("tenant = ? AND id = ?", tenant, id).
		UpdateColumns(map[string]interface{}{
			"provision_status": provisionStatus,
			"updated_at":       utils.Now(),
		}).Error
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	return nil
}

func (r *mailboxRepository) GetForConfiguration(ctx context.Context, limit int) ([]*models.Mailbox, error) {
	span, _ := opentracing.StartSpanFromContext(ctx, "mailboxRepository.GetForConfiguration")
	defer span.Finish()
	tracing.SetDefaultPostgresRepositorySpanTags(ctx, span)
	span.LogFields(tracingLog.Int("limit", limit))

	twoHoursAgo := utils.Now().Add(-2 * time.Hour)

	var result []*models.Mailbox
	err := r.db.WithContext(ctx).
		Where("provider = ?", enum.EmailMailstack).
		Where("provision_status = ? AND ("+
			"(configure_attempt_at IS NULL AND created_at < ?) OR "+
			"(configure_attempt_at < ?)"+
			")", models.MailboxStatusPendingProvisioning, twoHoursAgo, twoHoursAgo).
		Order("CASE WHEN configure_attempt_at IS NULL THEN 0 ELSE 1 END, configure_attempt_at ASC").
		Limit(limit).
		Find(&result).
		Error

	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}

	span.LogFields(tracingLog.Int("result.count", len(result)))
	return result, nil
}
