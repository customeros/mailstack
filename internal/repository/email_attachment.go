package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/opentracing/opentracing-go"
	"gorm.io/gorm"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"
)

type emailAttachmentRepository struct {
	db      *gorm.DB
	storage interfaces.StorageService
}

func NewEmailAttachmentRepository(db *gorm.DB, storageService interfaces.StorageService) interfaces.EmailAttachmentRepository {
	return &emailAttachmentRepository{
		db:      db,
		storage: storageService,
	}
}

// GetByID retrieves an attachment by its ID
func (r *emailAttachmentRepository) GetByID(ctx context.Context, id string) (*models.EmailAttachment, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailAttachmentRepository.GetByID")
	defer span.Finish()
	tracing.TagComponentPostgresRepository(span)

	var attachment models.EmailAttachment
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&attachment).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		tracing.TraceErr(span, err)
		return nil, err
	}
	return &attachment, nil
}

// ListByEmail retrieves all attachments for a specific email
func (r *emailAttachmentRepository) ListByEmail(ctx context.Context, emailID string) ([]*models.EmailAttachment, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailAttachmentRepository.ListByEmail")
	defer span.Finish()
	tracing.TagComponentPostgresRepository(span)

	var attachments []*models.EmailAttachment
	err := r.db.WithContext(ctx).
		Where("? = ANY(emails)", emailID).
		Find(&attachments).Error
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}
	return attachments, nil
}

// ListByThread retrieves all attachments for a specific email thread
func (r *emailAttachmentRepository) ListByThread(ctx context.Context, threadID string) ([]*models.EmailAttachment, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailAttachmentRepository.ListByThread")
	defer span.Finish()
	tracing.TagComponentPostgresRepository(span)

	var attachments []*models.EmailAttachment
	err := r.db.WithContext(ctx).
		Where("? = ANY(threads)", threadID).
		Find(&attachments).Error
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}
	return attachments, nil
}

func (r *emailAttachmentRepository) CheckFileExists(ctx context.Context, data []byte) (*models.EmailAttachment, string, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailAttachmentRepository.CheckFileExists")
	defer span.Finish()
	tracing.TagComponentPostgresRepository(span)

	hash := sha256.New()
	hash.Write(data)
	contentHash := hex.EncodeToString(hash.Sum(nil))
	var existingAttachment models.EmailAttachment
	err := r.db.WithContext(ctx).Where("content_hash = ?", contentHash).First(&existingAttachment).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			tracing.TraceErr(span, err)
			return nil, contentHash, err
		}
		return nil, contentHash, nil
	}
	return &existingAttachment, contentHash, nil
}

// Store saves attachment data to the configured storage service
func (r *emailAttachmentRepository) Store(ctx context.Context, attachment *models.EmailAttachment, threadID, emailID string, data []byte) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailAttachmentRepository.Store")
	defer span.Finish()
	tracing.TagComponentPostgresRepository(span)

	if attachment == nil {
		err := errors.New("Nil attachment")
		tracing.TraceErr(span, err)
		return err
	}

	existingAttachment, fileHash, err := r.CheckFileExists(ctx, data)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	// If file exists, update email and thread references
	if existingAttachment != nil {
		if !utils.IsStringInSlice(emailID, existingAttachment.Emails) {
			existingAttachment.Emails = append(existingAttachment.Emails, emailID)
		}
		if !utils.IsStringInSlice(threadID, existingAttachment.Threads) {
			existingAttachment.Threads = append(existingAttachment.Threads, threadID)
		}
		if attachment.Filename != "" {
			existingAttachment.Filename = attachment.Filename
		}
		return r.db.WithContext(ctx).Save(existingAttachment).Error
	}

	// This is a new file, proceed with upload
	attachment.ContentHash = fileHash
	attachment.Size = len(data)
	attachment.UpdatedAt = time.Now()
	attachment.Emails = []string{emailID}
	attachment.Threads = []string{threadID}

	fileExt := utils.GetFileExtensionFromContentType(attachment.ContentType)
	if attachment.ID == "" {
		attachment.ID = utils.GenerateNanoIDWithPrefix("file", 12)
	}
	filename := attachment.ID
	if fileExt != "other" && fileExt != "audio" && fileExt != "video" {
		filename = filename + "." + fileExt
	}

	attachment.StorageKey = fmt.Sprintf("%s/%s", fileExt, filename)

	// Store the file in the storage service
	if err := r.storage.Upload(ctx, attachment.StorageKey, data, attachment.ContentType); err != nil {
		tracing.TraceErr(span, err)
		tracing.LogObjectAsJson(span, "attachment", attachment)
		return fmt.Errorf("failed to upload attachment: %w", err)
	}

	return r.db.WithContext(ctx).Save(attachment).Error
}

// GetAttachment retrieves the attachment data from storage
func (r *emailAttachmentRepository) DownloadAttachment(ctx context.Context, id string) ([]byte, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailAttachmentRepository.GetData")
	defer span.Finish()
	tracing.TagComponentPostgresRepository(span)

	// Get the attachment metadata
	attachment, err := r.GetByID(ctx, id)
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}
	if attachment == nil {
		tracing.TraceErr(span, err)
		return nil, errors.New("attachment not found")
	}

	// Retrieve the file from storage
	data, err := r.storage.Download(ctx, attachment.StorageKey)
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, fmt.Errorf("failed to download attachment: %w", err)
	}

	return data, nil
}

// Delete removes an attachment from both database and storage
func (r *emailAttachmentRepository) Delete(ctx context.Context, id string) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailAttachmentRepository.Delete")
	defer span.Finish()
	tracing.TagComponentPostgresRepository(span)

	// Get the attachment first
	attachment, err := r.GetByID(ctx, id)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}
	if attachment == nil {
		return nil // Already deleted
	}

	// Delete from storage
	if attachment.StorageKey != "" {
		if err := r.storage.Delete(ctx, attachment.StorageKey); err != nil {
			// Log the error but continue with DB deletion
			fmt.Printf("Failed to delete attachment from storage: %v", err)
		}
	}

	// Delete from database
	return r.db.WithContext(ctx).Delete(&models.EmailAttachment{}, "id = ?", id).Error
}
