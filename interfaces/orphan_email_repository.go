package interfaces

import (
	"context"
	"time"

	"github.com/customeros/mailstack/internal/models"
)

type OrphanEmailRepository interface {
	Create(ctx context.Context, orphan *models.OrphanEmail) (string, error)
	GetByID(ctx context.Context, id string) (*models.OrphanEmail, error)
	GetByMessageID(ctx context.Context, messageID string) (*models.OrphanEmail, error)
	Delete(ctx context.Context, id string) error
	DeleteByThreadID(ctx context.Context, threadID string) error
	ListByThreadID(ctx context.Context, threadID string) ([]*models.OrphanEmail, error)
	ListByMailboxID(ctx context.Context, mailboxID string, limit, offset int) ([]*models.OrphanEmail, error)
	DeleteOlderThan(ctx context.Context, cutoffDate time.Time) error
}
