package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/telemetry"
	"github.com/customeros/mailstack/internal/utils"
)

type emailThreadRepository struct {
	db *gorm.DB
}

// NewEmailThreadRepository creates a new email thread repository
func NewEmailThreadRepository(db *gorm.DB) interfaces.EmailThreadRepository {
	return &emailThreadRepository{
		db: db,
	}
}

// Create inserts a new email thread into the database
func (r *emailThreadRepository) Create(ctx context.Context, thread *models.EmailThread) (string, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "emailThreadRepository.Create")
	defer spans.Finish()

	// Validate input
	if thread == nil {
		err := errors.New("thread cannot be nil")
		spans.TraceError(err)
		return "", err
	}

	// Generate ID if not provided
	if thread.ID == "" {
		thread.ID = utils.GenerateNanoIDWithPrefix("thrd", 16)
	}

	if thread.LastMessageID != "" {
		thread.LastMessageID = strings.Trim(thread.LastMessageID, "<>")
	}

	// Set timestamps
	now := utils.Now()
	thread.CreatedAt = now
	thread.UpdatedAt = now

	// Create the thread
	if err := r.db.WithContext(ctx).Create(thread).Error; err != nil {
		spans.TraceError(err)
		return "", err
	}

	return thread.ID, nil
}

// GetByID retrieves an email thread by its ID
func (r *emailThreadRepository) GetByID(ctx context.Context, id string) (*models.EmailThread, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "emailThreadRepository.GetByID")
	defer spans.Finish()
	spans.TagEntity(id)

	if id == "" {
		err := errors.New("thread ID cannot be empty")
		spans.TraceError(err)
		return nil, err
	}

	var thread models.EmailThread
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&thread).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFoundErr := fmt.Errorf("thread with ID %s not found", id)
			spans.TraceError(notFoundErr)
			return nil, notFoundErr
		}
		spans.TraceError(err)
		return nil, err
	}

	return &thread, nil
}

// GetByMailboxIDsPaginated retrieves threads for an array of mailboxes with pagination
func (r *emailThreadRepository) GetByMailboxIDs(ctx context.Context, mailboxIDs []string, limit int, offset int) ([]*models.EmailThread, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "emailThreadRepository.GetByMailboxIDsPaginated")
	defer spans.Finish()

	spans.LogKV("mailbox_ids", strings.Join(mailboxIDs, ","))
	spans.LogKV("limit", limit)
	spans.LogKV("offset", offset)

	if len(mailboxIDs) == 0 {
		err := errors.New("mailbox IDs list cannot be empty")
		spans.TraceError(err)
		return nil, err
	}

	var threads []*models.EmailThread

	err := r.db.WithContext(ctx).
		Where("mailbox_id IN ?", mailboxIDs).
		Order("last_message_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&threads).Error
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	return threads, nil
}

// CountByMailboxIDs counts total number of threads across all specified mailboxes
func (r *emailThreadRepository) CountByMailboxIDs(ctx context.Context, mailboxIDs []string) (int64, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "emailThreadRepository.CountByMailboxIDs")
	defer spans.Finish()

	spans.LogKV("mailbox_ids", strings.Join(mailboxIDs, ","))

	if len(mailboxIDs) == 0 {
		err := errors.New("mailbox IDs list cannot be empty")
		spans.TraceError(err)
		return 0, err
	}

	var count int64

	err := r.db.WithContext(ctx).
		Model(&models.EmailThread{}).
		Where("mailbox_id IN ?", mailboxIDs).
		Count(&count).Error
	if err != nil {
		spans.TraceError(err)
		return 0, err
	}

	return count, nil
}

// Update updates an existing email thread
func (r *emailThreadRepository) Update(ctx context.Context, thread *models.EmailThread) error {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "emailThreadRepository.Update")
	defer spans.Finish()

	// Validate input
	if thread == nil {
		err := errors.New("thread cannot be nil")
		spans.TraceError(err)
		return err
	}
	if thread.ID == "" {
		err := errors.New("thread ID cannot be empty")
		spans.TraceError(err)
		return err
	}
	spans.TagEntity(thread.ID)

	// Update timestamp
	thread.UpdatedAt = utils.Now()

	// Start a transaction
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		spans.TraceError(tx.Error)
		return tx.Error
	}

	// Check if thread exists
	var exists bool
	err := tx.Model(&models.EmailThread{}).
		Select("COUNT(*) > 0").
		Where("id = ?", thread.ID).
		Find(&exists).Error
	if err != nil {
		tx.Rollback()
		spans.TraceError(err)
		return err
	}
	if !exists {
		tx.Rollback()
		err := fmt.Errorf("thread with ID %s not found", thread.ID)
		spans.TraceError(err)
		return err
	}

	// Build updates map with only non-empty fields
	updates := map[string]interface{}{
		"updated_at": thread.UpdatedAt, // Always update the timestamp
	}

	// Only include fields that should be updated
	if thread.Subject != "" {
		updates["subject"] = thread.Subject
	}
	if len(thread.Participants) > 0 {
		updates["participants"] = thread.Participants
	}
	if thread.LastMessageID != "" {
		updates["last_message_id"] = strings.Trim(thread.LastMessageID, "<>")
	}

	// Conditionally include time pointers only if they're not nil
	if thread.LastMessageAt != nil {
		updates["last_message_at"] = thread.LastMessageAt
	}
	if thread.FirstMessageAt != nil {
		updates["first_message_at"] = thread.FirstMessageAt
	}

	// Only perform the update if we have fields to update
	if len(updates) > 1 { // More than just updated_at
		result := tx.Model(&models.EmailThread{}).
			Where("id = ?", thread.ID).
			Updates(updates)

		if result.Error != nil {
			tx.Rollback()
			spans.TraceError(result.Error)
			return result.Error
		}
	}

	// Commit the transaction
	if err := tx.Commit().Error; err != nil {
		spans.TraceError(err)
		return err
	}

	return nil
}

// IncrementMessageCount atomically updates a thread with a new message
func (r *emailThreadRepository) IncrementMessageCount(ctx context.Context, threadID string, messageID string, messageTime time.Time) error {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "emailThreadRepository.IncrementMessageCount")
	defer spans.Finish()
	spans.TagEntity(threadID)
	spans.TagString("message_id", messageID)

	if threadID == "" || messageID == "" {
		err := errors.New("thread ID and message ID cannot be empty")
		spans.TraceError(err)
		return err
	}

	// Start a transaction
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		spans.TraceError(tx.Error)
		return tx.Error
	}

	// Lock the thread record for update
	var thread models.EmailThread
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", threadID).
		First(&thread).Error
	if err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFoundErr := fmt.Errorf("thread with ID %s not found", threadID)
			spans.TraceError(notFoundErr)
			return notFoundErr
		}
		spans.TraceError(err)
		return err
	}

	// Update the thread
	updates := map[string]interface{}{
		"message_count":   gorm.Expr("message_count + 1"),
		"last_message_id": messageID,
		"updated_at":      utils.Now(),
	}

	// Only update last_message_at if the new message is more recent
	if thread.LastMessageAt == nil || messageTime.After(*thread.LastMessageAt) {
		updates["last_message_at"] = messageTime
	}

	// Only update first_message_at if it's not set or the new message is earlier
	if thread.FirstMessageAt == nil || messageTime.Before(*thread.FirstMessageAt) {
		updates["first_message_at"] = messageTime
	}

	result := tx.Model(&models.EmailThread{}).
		Where("id = ?", threadID).
		Updates(updates)

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

// GetParticipantsForThread retrieves the list of participants for a thread
func (r *emailThreadRepository) GetParticipantsForThread(ctx context.Context, threadID string) ([]string, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "emailThreadRepository.GetParticipantsForThread")
	defer spans.Finish()
	spans.TagEntity(threadID)

	if threadID == "" {
		err := errors.New("thread ID cannot be empty")
		spans.TraceError(err)
		return nil, err
	}

	var thread models.EmailThread
	err := r.db.WithContext(ctx).
		Select("participants").
		Where("id = ?", threadID).
		First(&thread).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFoundErr := fmt.Errorf("thread with ID %s not found", threadID)
			spans.TraceError(notFoundErr)
			return nil, notFoundErr
		}
		spans.TraceError(err)
		return nil, err
	}

	return thread.Participants, nil
}

// FindBySubjectAndMailbox finds threads with a matching subject and mailbox
func (r *emailThreadRepository) FindBySubjectAndMailbox(ctx context.Context, subject string, mailboxID string) ([]*models.EmailThread, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "EmailThreadRepository.FindBySubjectAndMailbox")
	defer spans.Finish()

	var threads []*models.EmailThread

	result := r.db.WithContext(ctx).
		Where("subject = ? AND mailbox_id = ?", subject, mailboxID).
		Order("last_message_at DESC").
		Find(&threads)

	if result.Error != nil {
		spans.TraceError(result.Error)
		return nil, errors.Wrap(result.Error, "error querying threads by subject")
	}

	return threads, nil
}

// MarkThreadAsViewed marks a thread as viewed
func (r *emailThreadRepository) MarkThreadAsViewed(ctx context.Context, threadID string) error {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "emailThreadRepository.MarkThreadAsViewed")
	defer spans.Finish()
	spans.TagEntity(threadID)

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

	// Check if thread exists
	var exists bool
	err := tx.Model(&models.EmailThread{}).
		Select("COUNT(*) > 0").
		Where("id = ?", threadID).
		Find(&exists).Error
	if err != nil {
		tx.Rollback()
		spans.TraceError(err)
		return err
	}
	if !exists {
		tx.Rollback()
		err := fmt.Errorf("thread with ID %s not found", threadID)
		spans.TraceError(err)
		return err
	}

	// Update the thread as viewed
	updates := map[string]interface{}{
		"is_viewed":  true,
		"updated_at": utils.Now(),
	}

	result := tx.Model(&models.EmailThread{}).
		Where("id = ?", threadID).
		Updates(updates)

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

// MarkThreadAsDone marks a thread as done
func (r *emailThreadRepository) MarkThreadAsDone(ctx context.Context, threadID string, isDone bool) error {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "emailThreadRepository.MarkThreadAsDone")
	defer spans.Finish()
	spans.TagEntity(threadID)
	spans.LogKV("is_done", isDone)

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

	// Check if thread exists
	var exists bool
	err := tx.Model(&models.EmailThread{}).
		Select("COUNT(*) > 0").
		Where("id = ?", threadID).
		Find(&exists).Error
	if err != nil {
		tx.Rollback()
		spans.TraceError(err)
		return err
	}
	if !exists {
		tx.Rollback()
		err := fmt.Errorf("thread with ID %s not found", threadID)
		spans.TraceError(err)
		return err
	}

	// Update the thread's done status
	updates := map[string]interface{}{
		"is_done":    isDone,
		"updated_at": utils.Now(),
	}

	result := tx.Model(&models.EmailThread{}).
		Where("id = ?", threadID).
		Updates(updates)

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
