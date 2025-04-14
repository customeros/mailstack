package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/customeros/mailstack/internal/telemetry"

	"gorm.io/gorm"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/utils"
)

type mailboxRepository struct {
	db *gorm.DB
}

// GetAllWithFilters implements interfaces.MailboxRepository.
func (r *mailboxRepository) GetAllWithFilters(ctx context.Context, provider enum.EmailProvider, domain string, userId string) ([]*models.Mailbox, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "mailboxRepository.GetAllWithFilters")
	defer spans.Finish()
	spans.LogKV("provider", provider, "domain", domain, "userId", userId)

	tenant := utils.GetTenantFromContext(ctx)

	// Start with base query for tenant
	query := r.db.WithContext(ctx).Where("tenant = ?", tenant)

	// Add optional filters
	if provider != "" {
		query = query.Where("provider = ?", provider)
	}
	if domain != "" {
		query = query.Where("mailbox_domain = ?", domain)
	}
	if userId != "" {
		query = query.Where("user_id = ?", userId)
	}

	var result []*models.Mailbox
	err := query.Find(&result).Error
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	spans.LogKV("result.count", len(result))
	return result, nil
}

func NewMailboxRepository(db *gorm.DB) interfaces.MailboxRepository {
	return &mailboxRepository{db: db}
}

func (r *mailboxRepository) GetMailboxes(ctx context.Context) ([]*models.Mailbox, error) {
	spans, _ := telemetry.StartPostgresSpan(ctx, "mailboxRepository.GetMailboxes")
	defer spans.Finish()

	var mailboxes []*models.Mailbox
	result := r.db.Find(&mailboxes)
	if result.Error != nil {
		spans.TraceError(result.Error)
		return nil, result.Error
	}
	spans.LogKV("result.count", len(mailboxes))
	return mailboxes, nil
}

func (r *mailboxRepository) GetMailboxesByUserID(ctx context.Context, userID string) ([]*models.Mailbox, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "mailboxRepository.GetMailboxesByUserID")
	defer spans.Finish()
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
	spans, _ := telemetry.StartPostgresSpan(ctx, "mailboxRepository.GetMailbox")
	defer spans.Finish()
	spans.TagEntity(id)

	var mailbox models.Mailbox
	err := r.db.First(&mailbox, "id = ?", id).Error
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	spans.LogKV("result.found", true)
	return &mailbox, nil
}

func (r *mailboxRepository) GetMailboxByEmailAddress(ctx context.Context, emailAddress string) (*models.Mailbox, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "mailboxRepository.GetMailboxByEmailAddress")
	defer spans.Finish()
	spans.LogKV("emailAddress", emailAddress)

	tenant := utils.GetTenantFromContext(ctx)

	var mailbox models.Mailbox
	err := r.db.First(&mailbox, "tenant = ? AND email_address = ?", tenant, emailAddress).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			spans.LogKV("result.found", false)
			return nil, nil
		}
		spans.TraceError(err)
		return nil, err
	}

	spans.LogKV("result.found", true)
	spans.LogKV("result.mailbox.id", mailbox.ID)
	return &mailbox, nil
}

func (r *mailboxRepository) GetMailboxByEmailAddressCrossTenant(ctx context.Context, emailAddress string) (*models.Mailbox, error) {
	spans, _ := telemetry.StartPostgresSpan(ctx, "mailboxRepository.GetMailboxByEmailAddressCrossTenant")
	defer spans.Finish()
	spans.LogKV("emailAddress", emailAddress)

	var mailbox models.Mailbox
	err := r.db.First(&mailbox, "email_address = ?", emailAddress).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			spans.LogKV("result.found", false)
			return nil, nil
		}
		spans.TraceError(err)
		return nil, err
	}

	spans.LogKV("result.found", true)
	spans.LogKV("result.mailbox.id", mailbox.ID)
	return &mailbox, nil
}

func (r *mailboxRepository) SaveMailbox(ctx context.Context, mailbox models.Mailbox) (string, error) {
	spans, _ := telemetry.StartPostgresSpan(ctx, "mailboxRepository.SaveMailbox")
	defer spans.Finish()

	// Perform the save operation
	result := r.db.Save(&mailbox)
	if result.Error != nil {
		spans.TraceError(result.Error)
		return "", result.Error
	}

	return mailbox.ID, nil
}

func (r *mailboxRepository) DeleteMailbox(ctx context.Context, id string) error {
	spans, _ := telemetry.StartPostgresSpan(ctx, "mailboxRepository.DeleteMailbox")
	defer spans.Finish()
	spans.TagEntity(id)

	return r.db.Delete(&models.Mailbox{}, "id = ?", id).Error
}

// UpdateConnectionStatus updates the connection status and error message for a mailbox
func (r *mailboxRepository) UpdateConnectionStatus(ctx context.Context, mailboxID string, status enum.ConnectionStatus, errorMessage string) error {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "mailboxRepository.UpdateConnectionStatus")
	defer spans.Finish()
	spans.TagEntity(mailboxID)
	spans.TagString("status", string(status))

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
		spans.TraceError(result.Error)
		return fmt.Errorf("failed to update mailbox connection status: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		err := fmt.Errorf("mailbox with ID %s not found", mailboxID)
		spans.TraceError(err)
		return err
	}

	spans.LogKV("affectedRows", result.RowsAffected)
	return nil
}

func (r *mailboxRepository) GetForRampUp(ctx context.Context) ([]*models.Mailbox, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "mailboxRepository.GetForRampUp")
	defer spans.Finish()

	var result []*models.Mailbox
	err := r.db.WithContext(ctx).
		Where("provider = ?", enum.EmailMailstack).
		Where("provision_status = ?", models.MailboxStatusProvisioned).
		Where("ramp_up_current < ramp_up_max and (last_ramp_up_at is null or last_ramp_up_at < ?)", utils.StartOfDayInUTC(utils.Now())).
		Find(&result).
		Error

	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	spans.LogKV("result.count", len(result))

	return result, nil
}

func (r *mailboxRepository) UpdateRampUpFields(ctx context.Context, mailbox *models.Mailbox) error {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "mailboxRepository.UpdateRampUpFields")
	defer spans.Finish()
	spans.TagEntity(mailbox.ID)
	spans.LogKV("ramp_up_current", mailbox.RampUpCurrent)
	spans.LogObjectAsJson("last_ramp_up_at", mailbox.LastRampUpAt)

	err := r.db.WithContext(ctx).
		Model(&models.Mailbox{}).
		Where("id = ?", mailbox.ID).
		Updates(map[string]interface{}{
			"ramp_up_current": mailbox.RampUpCurrent,
			"last_ramp_up_at": mailbox.LastRampUpAt,
		}).Error

	if err != nil {
		spans.TraceError(err)
		return err
	}

	return nil
}

func (r *mailboxRepository) ConfigureAttempt(ctx context.Context, id string) error {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "mailboxRepository.ConfigureAttempt")
	defer spans.Finish()
	spans.TagEntity(id)

	err := r.db.WithContext(ctx).
		Model(&models.Mailbox{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"configure_attempt_at": time.Now(),
		}).Error

	if err != nil {
		spans.TraceError(err)
		return err
	}

	return nil
}

func (r *mailboxRepository) UpdateProvisionStatus(ctx context.Context, id string, provisionStatus models.MailboxProvisionStatus) error {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "mailboxRepository.UpdateProvisionStatus")
	defer spans.Finish()
	spans.TagEntity(id)
	spans.LogKV("provisionStatus", provisionStatus)

	tenant := utils.GetTenantFromContext(ctx)

	err := r.db.WithContext(ctx).
		Model(&models.Mailbox{}).
		Where("tenant = ? AND id = ?", tenant, id).
		UpdateColumns(map[string]interface{}{
			"provision_status": provisionStatus,
			"updated_at":       utils.Now(),
		}).Error
	if err != nil {
		spans.TraceError(err)
		return err
	}

	return nil
}

func (r *mailboxRepository) GetForConfiguration(ctx context.Context, limit int) ([]*models.Mailbox, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "mailboxRepository.GetForConfiguration")
	defer spans.Finish()
	spans.LogKV("limit", limit)

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
		spans.TraceError(err)
		return nil, err
	}

	spans.LogKV("result.count", len(result))
	return result, nil
}

func (r *mailboxRepository) UpdateOauthToken(ctx context.Context, mailboxID, accessToken, refreshToken string, tokenExpiry *time.Time) error {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "mailboxRepository.UpdateOauthToken")
	defer spans.Finish()
	spans.TagEntity(mailboxID)

	err := r.db.WithContext(ctx).
		Model(&models.Mailbox{}).
		Where("id = ?", mailboxID).
		Updates(map[string]interface{}{
			"oauth_access_token":  accessToken,
			"oauth_refresh_token": refreshToken,
			"oauth_token_expiry":  tokenExpiry,
			"updated_at":          utils.Now(),
		}).Error

	if err != nil {
		spans.TraceError(err)
		return err
	}

	return nil
}

func (r *mailboxRepository) MarkForManualRefresh(ctx context.Context, mailboxID string, needsManualRefresh bool) error {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "mailboxRepository.MarkForManualRefresh")
	defer spans.Finish()
	spans.TagEntity(mailboxID)
	spans.LogKV("needsManualRefresh", needsManualRefresh)

	err := r.db.WithContext(ctx).
		Model(&models.Mailbox{}).
		Where("id = ?", mailboxID).
		Update("oauth_needs_manual_refresh", needsManualRefresh).Error

	if err != nil {
		spans.TraceError(err)
		return err
	}

	return nil
}

func (r *mailboxRepository) GetMailboxesForSync(ctx context.Context, staleTimeout time.Duration) ([]*models.Mailbox, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "mailboxRepository.GetMailboxesForSync")
	defer spans.Finish()
	spans.LogKV("staleTimeout", staleTimeout)

	var result []*models.Mailbox
	err := r.db.WithContext(ctx).
		Where("provision_status = ? ", models.MailboxStatusProvisioned).
		Where("inbound_enabled = ?", true).
		Where("provider = ? OR provider = ?", enum.EmailMailstack, enum.EmailGoogleWorkspace).
		Where("processing_pod_id = ? OR processing_heartbeat_at IS NULL OR processing_heartbeat_at < ?", "", utils.Now().Add(-staleTimeout)).
		Order("RANDOM()").
		Find(&result).
		Error

	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	spans.LogKV("result.count", len(result))
	return result, nil
}

func (r *mailboxRepository) AcquireMailboxLock(ctx context.Context, mailboxID, podID string, staleTimeout time.Duration) (bool, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "mailboxRepository.AcquireMailboxLock")
	defer spans.Finish()
	spans.TagEntity(mailboxID)
	spans.LogKV("podID", podID)

	now := utils.Now()
	result := r.db.WithContext(ctx).Exec(`
		UPDATE mailboxes 
		SET processing_pod_id = ?,
			processing_started_at = ?,
			processing_heartbeat_at = ?,
			processing_run_count = 0
		WHERE id = ? 
		AND (
			processing_pod_id IS NULL OR processing_pod_id = ? OR
			processing_heartbeat_at IS NULL OR processing_heartbeat_at < ?
		)
		RETURNING id
	`, podID, now, now, mailboxID, "", now.Add(-staleTimeout))

	if result.Error != nil {
		spans.TraceError(result.Error)
		return false, result.Error
	}

	spans.LogKV("result.lockAcquired", result.RowsAffected > 0)
	return result.RowsAffected > 0, nil
}

func (r *mailboxRepository) UpdateMailboxHeartbeat(ctx context.Context, mailboxID, podID string) error {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "mailboxRepository.UpdateMailboxHeartbeat")
	defer spans.Finish()
	spans.TagEntity(mailboxID)
	spans.LogKV("podID", podID)

	result := r.db.WithContext(ctx).Exec(`
		UPDATE mailboxes 
		SET processing_heartbeat_at = ?,
			processing_run_count = processing_run_count + 1
		WHERE id = ? 
		AND processing_pod_id = ?
		RETURNING id
	`, utils.Now(), mailboxID, podID)

	if result.Error != nil {
		spans.TraceError(result.Error)
		return result.Error
	}

	return nil
}

func (r *mailboxRepository) ReleaseMailboxLock(ctx context.Context, mailboxID, podID string) error {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "mailboxRepository.ReleaseMailboxLock")
	defer spans.Finish()
	spans.TagEntity(mailboxID)
	spans.LogKV("podID", podID)

	result := r.db.WithContext(ctx).Exec(`
		UPDATE mailboxes 
		SET processing_pod_id = NULL,
			processing_started_at = NULL,
			processing_heartbeat_at = NULL,
			processing_run_count = 0
		WHERE id = ? 
		AND processing_pod_id = ?
		RETURNING id
	`, mailboxID, podID)

	if result.Error != nil {
		spans.TraceError(result.Error)
		return result.Error
	}

	return nil
}
