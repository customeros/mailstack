package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/telemetry"
)

type orphanEmailRepository struct {
	db *gorm.DB
}

// NewOrphanEmailRepository creates a new orphan email repository
func NewOrphanEmailRepository(db *gorm.DB) interfaces.OrphanEmailRepository {
	return &orphanEmailRepository{
		db: db,
	}
}

// Create inserts a new orphan email record into the database
func (r *orphanEmailRepository) Create(ctx context.Context, orphan *models.OrphanEmail) (string, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "orphanEmailRepository.Create")
	defer spans.Finish()

	// Validate input
	if orphan == nil {
		err := errors.New("orphan email cannot be nil")
		spans.TraceError(err)
		return "", err
	}

	if orphan.ReferencedBy != "" {
		orphan.ReferencedBy = strings.Trim(orphan.ReferencedBy, "<>")
	}

	// Set creation timestamp if not already set
	if orphan.CreatedAt.IsZero() {
		orphan.CreatedAt = time.Now()
	}

	// Start a transaction
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		spans.TraceError(tx.Error)
		return "", tx.Error
	}

	// Check if an entry with this message ID already exists
	var count int64
	if err := tx.Model(&models.OrphanEmail{}).
		Where("message_id = ?", orphan.MessageID).
		Count(&count).Error; err != nil {
		tx.Rollback()
		spans.TraceError(err)
		return "", err
	}

	// If it exists, return
	if count > 0 {
		return "", nil
	}

	// Create the new record
	if err := tx.Create(orphan).Error; err != nil {
		tx.Rollback()
		spans.TraceError(err)
		return "", err
	}

	// Commit the transaction
	if err := tx.Commit().Error; err != nil {
		spans.TraceError(err)
		return "", err
	}

	return orphan.ID, nil
}

// GetByID retrieves an orphan email by its ID
func (r *orphanEmailRepository) GetByID(ctx context.Context, id string) (*models.OrphanEmail, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "orphanEmailRepository.GetByID")
	defer spans.Finish()
	spans.TagEntity(id)

	if id == "" {
		err := errors.New("orphan ID cannot be empty")
		spans.TraceError(err)
		return nil, err
	}

	var orphan models.OrphanEmail
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&orphan).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFoundErr := fmt.Errorf("orphan with ID %s not found", id)
			spans.TraceError(notFoundErr)
			return nil, notFoundErr
		}
		spans.TraceError(err)
		return nil, err
	}

	return &orphan, nil
}

// GetByMessageID retrieves orphan emails by message ID
func (r *orphanEmailRepository) GetByMessageID(ctx context.Context, messageID string) (*models.OrphanEmail, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "orphanEmailRepository.GetByMessageID")
	defer spans.Finish()
	spans.TagEntity(messageID)

	if messageID == "" {
		err := errors.New("message ID cannot be empty")
		spans.TraceError(err)
		return nil, err
	}

	var orphans *models.OrphanEmail
	err := r.db.WithContext(ctx).Where("message_id = ?", messageID).First(&orphans).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		spans.TraceError(err)
		return nil, err
	}

	return orphans, nil
}

// Delete removes an orphan email by its ID
func (r *orphanEmailRepository) Delete(ctx context.Context, id string) error {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "orphanEmailRepository.Delete")
	defer spans.Finish()
	spans.TagEntity(id)

	if id == "" {
		err := errors.New("orphan ID cannot be empty")
		spans.TraceError(err)
		return err
	}

	// Start a transaction
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		spans.TraceError(tx.Error)
		return tx.Error
	}

	// Delete the orphan
	result := tx.Delete(&models.OrphanEmail{}, "id = ?", id)
	if result.Error != nil {
		tx.Rollback()
		spans.TraceError(result.Error)
		return result.Error
	}

	// Check if any rows were affected
	if result.RowsAffected == 0 {
		tx.Rollback()
		err := fmt.Errorf("orphan with ID %s not found", id)
		spans.TraceError(err)
		return err
	}

	// Commit the transaction
	if err := tx.Commit().Error; err != nil {
		spans.TraceError(err)
		return err
	}

	return nil
}

// DeleteByMessageID removes all orphan emails with the given message ID
func (r *orphanEmailRepository) DeleteByThreadID(ctx context.Context, threadID string) error {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "orphanEmailRepository.DeleteByThreadID")
	defer spans.Finish()
	spans.TagString("threadID", threadID)

	if threadID == "" {
		err := errors.New("thread ID cannot be empty")
		spans.TraceError(err)
		return err
	}

	// Start a transaction
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		spans.TraceError(tx.Error)
		return tx.Error
	}

	// Delete all orphans with this message ID
	result := tx.Delete(&models.OrphanEmail{}, "thread_id = ?", threadID)
	if result.Error != nil {
		tx.Rollback()
		spans.TraceError(result.Error)
		return result.Error
	}

	// Commit the transaction
	if err := tx.Commit().Error; err != nil {
		spans.TraceError(err)
		return err
	}

	return nil
}

// ListByThreadID retrieves orphan emails by thread ID
func (r *orphanEmailRepository) ListByThreadID(ctx context.Context, threadID string) ([]*models.OrphanEmail, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "orphanEmailRepository.ListByThreadID")
	defer spans.Finish()
	spans.TagString("threadID", threadID)

	if threadID == "" {
		err := errors.New("thread ID cannot be empty")
		spans.TraceError(err)
		return nil, err
	}

	var orphans []*models.OrphanEmail
	err := r.db.WithContext(ctx).
		Where("thread_id = ?", threadID).
		Order("created_at DESC").
		Find(&orphans).Error
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	return orphans, nil
}

// ListByMailboxID retrieves orphan emails by mailbox ID with pagination
func (r *orphanEmailRepository) ListByMailboxID(ctx context.Context, mailboxID string, limit, offset int) ([]*models.OrphanEmail, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "orphanEmailRepository.ListByMailboxID")
	defer spans.Finish()
	spans.TagString("mailboxID", mailboxID)
	spans.LogKV("limit", limit)
	spans.LogKV("offset", offset)

	if mailboxID == "" {
		err := errors.New("mailbox ID cannot be empty")
		spans.TraceError(err)
		return nil, err
	}

	// Set default limit if not provided
	if limit <= 0 {
		limit = 50
	}

	var orphans []*models.OrphanEmail
	err := r.db.WithContext(ctx).
		Where("mailbox_id = ?", mailboxID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&orphans).Error
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	return orphans, nil
}

// DeleteOlderThan removes orphan emails older than the specified date
func (r *orphanEmailRepository) DeleteOlderThan(ctx context.Context, cutoffDate time.Time) error {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "orphanEmailRepository.DeleteOlderThan")
	defer spans.Finish()
	spans.LogKV("cutoffDate", cutoffDate.Format(time.RFC3339))

	// Start a transaction
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		spans.TraceError(tx.Error)
		return tx.Error
	}

	// Delete orphans older than the cutoff date
	result := tx.Delete(&models.OrphanEmail{}, "created_at < ?", cutoffDate)
	if result.Error != nil {
		tx.Rollback()
		spans.TraceError(result.Error)
		return result.Error
	}

	// Commit the transaction
	if err := tx.Commit().Error; err != nil {
		spans.TraceError(err)
		return err
	}

	spans.LogKV("deleted_count", result.RowsAffected)
	return nil
}
