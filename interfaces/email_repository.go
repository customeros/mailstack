package interfaces

import (
	"context"

	"github.com/customeros/mailstack/internal/models"
)

type EmailRepository interface {
	Create(ctx context.Context, email *models.Email) error
	GetByID(ctx context.Context, id string) (*models.Email, error)
	GetByUID(ctx context.Context, mailboxID, folder string, uid uint32) (*models.Email, error)
	GetByMessageID(ctx context.Context, messageID string) (*models.Email, error)
	ListByMailbox(ctx context.Context, mailboxID string, limit, offset int) ([]*models.Email, int64, error)
	ListByFolder(ctx context.Context, mailboxID, folder string, limit, offset int) ([]*models.Email, int64, error)
	ListByThread(ctx context.Context, threadID string) ([]*models.Email, error)
	Search(ctx context.Context, query string, limit, offset int) ([]*models.Email, int64, error)
	Update(ctx context.Context, email *models.Email) error
}
