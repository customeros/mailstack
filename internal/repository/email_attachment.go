package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/telemetry"
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
	spans, ctx := telemetry.StartPostgresSpan(ctx, "emailAttachmentRepository.GetByID")
	defer spans.Finish()

	var attachment models.EmailAttachment
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&attachment).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		spans.TraceError(err)
		return nil, err
	}
	return &attachment, nil
}

// ListByEmail retrieves all attachments for a specific email
func (r *emailAttachmentRepository) ListByEmail(ctx context.Context, emailID string) ([]*models.EmailAttachment, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "emailAttachmentRepository.ListByEmail")
	defer spans.Finish()

	var attachments []*models.EmailAttachment
	err := r.db.WithContext(ctx).
		Where("? = ANY(emails)", emailID).
		Find(&attachments).Error
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}
	return attachments, nil
}

// ListByThread retrieves all attachments for a specific email thread
func (r *emailAttachmentRepository) ListByThread(ctx context.Context, threadID string) ([]*models.EmailAttachment, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "emailAttachmentRepository.ListByThread")
	defer spans.Finish()

	var attachments []*models.EmailAttachment
	err := r.db.WithContext(ctx).
		Where("? = ANY(threads)", threadID).
		Find(&attachments).Error
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}
	return attachments, nil
}

func (r *emailAttachmentRepository) CheckFileExists(ctx context.Context, contentHash string) (*models.EmailAttachment, string, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "emailAttachmentRepository.CheckFileExists")
	defer spans.Finish()

	var existingAttachment models.EmailAttachment
	err := r.db.WithContext(ctx).Where("content_hash = ?", contentHash).First(&existingAttachment).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			spans.TraceError(err)
			return nil, contentHash, err
		}
		return nil, contentHash, nil
	}
	return &existingAttachment, contentHash, nil
}

// Store saves attachment data to the configured storage service
func (r *emailAttachmentRepository) Store(ctx context.Context, attachment *models.EmailAttachment, emailID, mailboxID string, data []byte) (string, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "emailAttachmentRepository.Store")
	defer spans.Finish()
	spans.TagEntity(emailID)
	spans.LogKV("mailboxID", mailboxID)

	if attachment == nil {
		err := errors.New("nil attachment")
		spans.TraceError(err)
		return "", err
	}

	hash := sha256.Sum256(data)
	fileHash := hex.EncodeToString(hash[:24])

	existingAttachment, fileHash, err := r.CheckFileExists(ctx, fileHash)
	if err != nil {
		spans.TraceError(err)
		return "", err
	}

	// If file exists, update email and thread references
	// If file exists, update email and thread references
	if existingAttachment != nil {
		if !utils.IsStringInSlice(emailID, existingAttachment.EmailIDs) {
			existingAttachment.EmailIDs = append(existingAttachment.EmailIDs, emailID)
		}
		if attachment.Filename != "" {
			existingAttachment.Filename = attachment.Filename
		}
		err := r.db.WithContext(ctx).Save(existingAttachment).Error
		return existingAttachment.ID, err
	}

	// This is a new file, proceed with upload
	attachment.ContentHash = fileHash
	attachment.Size = len(data)
	attachment.UpdatedAt = time.Now()
	attachment.EmailIDs = []string{emailID}

	fileExt := utils.GetFileExtensionFromContentType(attachment.ContentType)
	if attachment.ID == "" {
		attachment.ID = utils.GenerateNanoIDWithPrefix("file", 12)
	}
	filename := attachment.ID
	if fileExt != "other" && fileExt != "audio" && fileExt != "video" {
		filename = filename + "." + fileExt
	}
	if mailboxID == "" {
		mailboxID = "default"
	}

	attachment.StorageKey = fmt.Sprintf("%s/%s/%s", mailboxID, fileExt, filename)

	// Store the file in the storage service
	if err := r.storage.Upload(ctx, attachment.StorageKey, data, attachment.ContentType); err != nil {
		spans.TraceError(err)
		spans.LogObjectAsJson("attachment", attachment)
		return "", fmt.Errorf("failed to upload attachment: %w", err)
	}

	return attachment.ID, r.db.WithContext(ctx).Save(attachment).Error
}

// GetAttachment retrieves the attachment data from storage
func (r *emailAttachmentRepository) DownloadAttachment(ctx context.Context, id string) ([]byte, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "emailAttachmentRepository.GetData")
	defer spans.Finish()

	// Get the attachment metadata
	attachment, err := r.GetByID(ctx, id)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}
	if attachment == nil {
		spans.TraceError(err)
		return nil, errors.New("attachment not found")
	}

	// Retrieve the file from storage
	data, err := r.storage.Download(ctx, attachment.StorageKey)
	if err != nil {
		spans.TraceError(err)
		return nil, fmt.Errorf("failed to download attachment: %w", err)
	}

	return data, nil
}

// Delete removes an attachment from both database and storage
func (r *emailAttachmentRepository) Delete(ctx context.Context, id string) error {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "emailAttachmentRepository.Delete")
	defer spans.Finish()

	// Get the attachment first
	attachment, err := r.GetByID(ctx, id)
	if err != nil {
		spans.TraceError(err)
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
