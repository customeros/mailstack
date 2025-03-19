package interfaces

import (
	"context"

	"github.com/customeros/mailstack/internal/models"
)

type EmailAttachmentRepository interface {
	GetByID(ctx context.Context, id string) (*models.EmailAttachment, error)
	ListByEmail(ctx context.Context, emailID string) ([]*models.EmailAttachment, error)
	ListByThread(ctx context.Context, threadID string) ([]*models.EmailAttachment, error)
	Store(ctx context.Context, attachment *models.EmailAttachment, threadID, emailID string, data []byte) error
	DownloadAttachment(ctx context.Context, id string) ([]byte, error)
	Delete(ctx context.Context, id string) error
}
