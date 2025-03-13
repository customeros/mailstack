package models

import (
	"time"

	"gorm.io/gorm"

	"github.com/customeros/mailstack/internal/utils"
)

// MailboxSyncState represents the synchronization state for a mailbox folder
type MailboxSyncState struct {
	ID         string    `gorm:"column:id;type:varchar(50);primaryKey"`
	MailboxID  string    `gorm:"column:mailbox_id;type:varchar(50);index;not null"`
	FolderName string    `gorm:"column:folder_name;type:varchar(100);index;not null"`
	LastUID    uint32    `gorm:"column:last_uid;not null"`
	LastSync   time.Time `gorm:"column:last_sync;type:timestamp;not null"`
	CreatedAt  time.Time `gorm:"column:created_at;type:timestamp;default:current_timestamp"`
	UpdatedAt  time.Time `gorm:"column:updated_at;type:timestamp;default:current_timestamp"`
}

func (MailboxSyncState) TableName() string {
	return "mailbox_sync_states"
}

func (m *MailboxSyncState) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = utils.GenerateNanoIDWithPrefix("sync", 12)
	}
	return nil
}
