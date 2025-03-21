package interfaces

import (
	"context"

	"github.com/customeros/mailstack/internal/models"
)

type EmailThreadRepository interface {
	Create(ctx context.Context, thread *models.EmailThread) (string, error)
	GetByID(ctx context.Context, id string) (*models.EmailThread, error)
	GetByMailboxIDs(ctx context.Context, mailboxIDs []string, limit int, offset int) ([]*models.EmailThread, error)
	CountByMailboxIDs(ctx context.Context, mailboxIDs []string) (int64, error)
	Update(ctx context.Context, thread *models.EmailThread) error
	GetParticipantsForThread(ctx context.Context, threadID string) ([]string, error)
	FindBySubjectAndMailbox(ctx context.Context, subject string, mailboxID string) ([]*models.EmailThread, error)
	MarkThreadAsViewed(ctx context.Context, threadID string) error
	MarkThreadAsDone(ctx context.Context, threadID string, isDone bool) error
}
