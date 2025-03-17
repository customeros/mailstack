package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/opentracing/opentracing-go"
	"gorm.io/gorm"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"
)

type emailRepository struct {
	db *gorm.DB
}

func NewEmailRepository(db *gorm.DB) interfaces.EmailRepository {
	return &emailRepository{
		db: db,
	}
}

func (r *emailRepository) Create(ctx context.Context, email *models.Email) (string, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailRepository.Create")
	defer span.Finish()
	tracing.TagComponentPostgresRepository(span)

	if email == nil {
		return "", nil
	}

	if email.MessageID != "" {
		email.MessageID = strings.Trim(email.MessageID, "<>")
	}

	if email.Subject != "" {
		email.CleanSubject = utils.NormalizeSubject(email.Subject)
	}

	// Check if email already exists before creating
	existingEmail := &models.Email{}
	err := r.db.WithContext(ctx).
		Where("message_id = ?", email.MessageID).
		First(existingEmail).Error

	if err == nil {
		// Email already exists
		span.SetTag("duplicate", true)
		return existingEmail.ID, nil // Return the ID of the existing email
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		// Some other error occurred
		tracing.TraceErr(span, err)
		return "", err
	}

	// Create the email if it doesn't exist
	result := r.db.WithContext(ctx).Create(email)
	if result.Error != nil {
		tracing.TraceErr(span, result.Error)
		return "", result.Error
	}

	return email.ID, nil
}

// GetByID retrieves an email by its ID
func (r *emailRepository) GetByID(ctx context.Context, id string) (*models.Email, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailRepository.GetByID")
	defer span.Finish()
	tracing.TagComponentPostgresRepository(span)

	var email models.Email
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&email).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		tracing.TraceErr(span, err)
		return nil, err
	}
	return &email, nil
}

// GetByUID retrieves an email by its UID within a specific mailbox and folder
func (r *emailRepository) GetByUID(ctx context.Context, mailboxID, folder string, uid uint32) (*models.Email, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailRepository.GetByUID")
	defer span.Finish()
	tracing.TagComponentPostgresRepository(span)

	var email models.Email
	if err := r.db.WithContext(ctx).
		Where("mailbox_id = ? AND folder = ? AND uid = ?", mailboxID, folder, uid).
		First(&email).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		tracing.TraceErr(span, err)
		return nil, err
	}
	return &email, nil
}

// GetByMessageID retrieves an email by its Message-ID header
func (r *emailRepository) GetByMessageID(ctx context.Context, messageID string) (*models.Email, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailRepository.GetByMessageID")
	defer span.Finish()
	tracing.TagComponentPostgresRepository(span)

	messageID = strings.Trim(messageID, "<>")

	var email models.Email
	if err := r.db.WithContext(ctx).Where("message_id = ?", messageID).First(&email).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		tracing.TraceErr(span, err)
		return nil, err
	}
	return &email, nil
}

// ListByMailbox retrieves emails for a specific mailbox with pagination
func (r *emailRepository) ListByMailbox(ctx context.Context, mailboxID string, limit, offset int) ([]*models.Email, int64, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailRepository.ListByMailbox")
	defer span.Finish()
	tracing.TagComponentPostgresRepository(span)

	var emails []*models.Email
	var count int64

	// Count total emails
	if err := r.db.WithContext(ctx).Model(&models.Email{}).
		Where("mailbox_id = ?", mailboxID).
		Count(&count).Error; err != nil {
		tracing.TraceErr(span, err)
		return nil, 0, err
	}

	// Get paginated emails
	if err := r.db.WithContext(ctx).
		Where("mailbox_id = ?", mailboxID).
		Order("received_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&emails).Error; err != nil {
		tracing.TraceErr(span, err)
		return nil, 0, err
	}

	return emails, count, nil
}

// ListByFolder retrieves emails for a specific mailbox and folder with pagination
func (r *emailRepository) ListByFolder(ctx context.Context, mailboxID, folder string, limit, offset int) ([]*models.Email, int64, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailRepository.ListByFolder")
	defer span.Finish()
	tracing.TagComponentPostgresRepository(span)

	var emails []*models.Email
	var count int64

	// Count total emails in folder
	if err := r.db.WithContext(ctx).Model(&models.Email{}).
		Where("mailbox_id = ? AND folder = ?", mailboxID, folder).
		Count(&count).Error; err != nil {
		tracing.TraceErr(span, err)
		return nil, 0, err
	}

	// Get paginated emails from folder
	if err := r.db.WithContext(ctx).
		Where("mailbox_id = ? AND folder = ?", mailboxID, folder).
		Order("received_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&emails).Error; err != nil {
		tracing.TraceErr(span, err)
		return nil, 0, err
	}

	return emails, count, nil
}

// ListByThread retrieves all emails in a conversation thread
func (r *emailRepository) ListByThread(ctx context.Context, threadID string) ([]*models.Email, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailRepository.ListByThread")
	defer span.Finish()
	tracing.TagComponentPostgresRepository(span)

	var emails []*models.Email

	if err := r.db.WithContext(ctx).
		Where("thread_id = ?", threadID).
		Order("received_at ASC").
		Find(&emails).Error; err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}

	return emails, nil
}

// Search searches emails by query string
func (r *emailRepository) Search(ctx context.Context, query string, limit, offset int) ([]*models.Email, int64, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailRepository.Search")
	defer span.Finish()
	tracing.TagComponentPostgresRepository(span)

	var emails []*models.Email
	var count int64

	// Build search condition - search in subject, body, sender, recipients
	searchCondition := "subject ILIKE ? OR body_text ILIKE ? OR from_address ILIKE ? OR from_name ILIKE ?"
	searchParam := "%" + query + "%"

	// Count total matching emails
	if err := r.db.WithContext(ctx).Model(&models.Email{}).
		Where(searchCondition, searchParam, searchParam, searchParam, searchParam).
		Count(&count).Error; err != nil {
		tracing.TraceErr(span, err)
		return nil, 0, err
	}

	// Get paginated search results
	if err := r.db.WithContext(ctx).
		Where(searchCondition, searchParam, searchParam, searchParam, searchParam).
		Order("received_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&emails).Error; err != nil {
		tracing.TraceErr(span, err)
		return nil, 0, err
	}

	return emails, count, nil
}

// Update updates an email record
func (r *emailRepository) Update(ctx context.Context, email *models.Email) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailRepository.Update")
	defer span.Finish()
	tracing.TagComponentPostgresRepository(span)

	if email == nil {
		err := errors.New("email cannot be nil")
		tracing.TraceErr(span, err)
		return err
	}

	if email.ID == "" {
		err := errors.New("email ID cannot be empty")
		tracing.TraceErr(span, err)
		return err
	}

	// Update timestamp
	email.UpdatedAt = time.Now()

	// Start a transaction
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		tracing.TraceErr(span, tx.Error)
		return tx.Error
	}

	// Perform update within transaction
	// First verify the record exists
	var exists bool
	err := tx.Model(&models.Email{}).
		Select("COUNT(*) > 0").
		Where("id = ?", email.ID).
		Find(&exists).Error
	if err != nil {
		tx.Rollback()
		tracing.TraceErr(span, err)
		return err
	}

	if !exists {
		tx.Rollback()
		err := fmt.Errorf("email with ID %s not found", email.ID)
		tracing.TraceErr(span, err)
		return err
	}

	// Update only specific fields to prevent overwriting data that shouldn't change
	result := tx.Model(&models.Email{}).
		Where("id = ?", email.ID).
		Updates(map[string]interface{}{
			// Only include fields that should be updatable
			"status":          email.Status,
			"status_detail":   email.StatusDetail,
			"sent_at":         email.SentAt,
			"last_attempt_at": email.LastAttemptAt,
			"scheduled_for":   email.ScheduledFor,
			"updated_at":      email.UpdatedAt,
			"send_attempts":   email.SendAttempts,
		})

	if result.Error != nil {
		tx.Rollback()
		tracing.TraceErr(span, result.Error)
		return result.Error
	}

	// Check if any rows were affected (should be 1)
	if result.RowsAffected != 1 {
		tx.Rollback()
		err := fmt.Errorf("expected to update 1 row, but updated %d", result.RowsAffected)
		tracing.TraceErr(span, err)
		return err
	}

	// Commit the transaction
	if err := tx.Commit().Error; err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	return nil
}

// SetEmailRawData updates only the raw data fields of an email (headers, envelope, body structure)
func (r *emailRepository) SetEmailRawData(ctx context.Context, emailID string, headers, envelope, bodyStructure models.JSONMap) error {
	if emailID == "" {
		return ErrInvalidInput
	}

	// Check if any of the fields needs updating
	if headers == nil && envelope == nil && bodyStructure == nil {
		return ErrInvalidInput
	}

	// Prepare update map with only the fields that are provided
	updates := map[string]interface{}{
		"updated_at": time.Now(),
	}

	if headers != nil {
		updates["raw_headers"] = headers
	}

	if envelope != nil {
		updates["envelope"] = envelope
	}

	if bodyStructure != nil {
		updates["body_structure"] = bodyStructure
	}

	// Execute the update
	result := r.db.WithContext(ctx).
		Model(&models.Email{}).
		Where("id = ?", emailID).
		Updates(updates)

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return ErrEmailNotFound
	}

	return nil
}
