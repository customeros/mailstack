package interfaces

import (
	"context"

	"github.com/customeros/mailstack/internal/models"
)

type MailboxSyncRepository interface {
	GetSyncState(ctx context.Context, mailboxID, folderName string) (*models.MailboxSyncState, error)
	SaveSyncState(ctx context.Context, state *models.MailboxSyncState) error
	DeleteSyncState(ctx context.Context, mailboxID, folderName string) error
	DeleteMailboxSyncStates(ctx context.Context, mailboxID string) error
	GetAllSyncStates(ctx context.Context) (map[string]map[string]uint32, error)
	GetMailboxSyncStates(ctx context.Context, mailboxID string) (map[string]uint32, error)
}
