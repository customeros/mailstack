package interfaces

import (
	"context"
	"time"

	"github.com/customeros/mailstack/internal/models"
)

type EmailThreadRepository interface {
	Create(ctx context.Context, thread *models.EmailThread) (string, error)
	GetByID(ctx context.Context, id string) (*models.EmailThread, error)
	Update(ctx context.Context, thread *models.EmailThread) error
	List(ctx context.Context, mailboxID string, limit, offset int) ([]*models.EmailThread, error)
	GetByMessageID(ctx context.Context, messageID string) (*models.EmailThread, error)
	IncrementMessageCount(ctx context.Context, threadID string, messageID string, messageTime time.Time) error
	GetParticipantsForThread(ctx context.Context, threadID string) ([]string, error)
}
