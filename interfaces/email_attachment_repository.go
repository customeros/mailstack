package interfaces

import (
	"context"

	"github.com/customeros/mailstack/internal/models"
)

type EmailAttachmentRepository interface {
	Create(ctx context.Context, attachment *models.EmailAttachment) error
	GetByID(ctx context.Context, id string) (*models.EmailAttachment, error)
	ListByEmail(ctx context.Context, emailID string) ([]*models.EmailAttachment, error)
	Store(ctx context.Context, attachment *models.EmailAttachment, data []byte) error
	GetData(ctx context.Context, id string) ([]byte, error)
	Delete(ctx context.Context, id string) error
}
