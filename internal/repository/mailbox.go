package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/opentracing/opentracing-go"
	"gorm.io/gorm"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/tracing"
)

type mailboxRepository struct {
	db *gorm.DB
}

func NewMailboxRepository(db *gorm.DB) interfaces.MailboxRepository {
	return &mailboxRepository{db: db}
}

func (r *mailboxRepository) GetMailboxes(ctx context.Context) ([]*models.Mailbox, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "mailboxRepository.GetMailboxes")
	defer span.Finish()
	tracing.TagComponentPostgresRepository(span)

	var mailboxes []*models.Mailbox
	result := r.db.Find(&mailboxes)
	if result.Error != nil {
		tracing.TraceErr(span, result.Error)
		return nil, result.Error
	}
	return mailboxes, nil
}

func (r *mailboxRepository) GetMailbox(ctx context.Context, id string) (*models.Mailbox, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "mailboxRepository.GetMailbox")
	defer span.Finish()
	tracing.TagComponentPostgresRepository(span)

	var mailbox models.Mailbox
	err := r.db.First(&mailbox, "id = ?", id).Error
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}
	return &mailbox, nil
}

func (r *mailboxRepository) GetMailboxByEmailAddress(ctx context.Context, emailAddress string) (*models.Mailbox, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "mailboxRepository.GetMailboxByEmailAddress")
	defer span.Finish()
	tracing.TagComponentPostgresRepository(span)

	var mailbox models.Mailbox
	err := r.db.First(&mailbox, "email_address = ?", emailAddress).Error
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}
	return &mailbox, nil
}

func (r *mailboxRepository) SaveMailbox(ctx context.Context, mailbox models.Mailbox) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "mailboxRepository.SaveMailbox")
	defer span.Finish()
	tracing.TagComponentPostgresRepository(span)

	return r.db.Save(&mailbox).Error
}

func (r *mailboxRepository) DeleteMailbox(ctx context.Context, id string) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "mailboxRepository.DeleteMailbox")
	defer span.Finish()
	tracing.TagComponentPostgresRepository(span)

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
