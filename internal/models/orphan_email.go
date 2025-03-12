package models

import (
	"time"

	"gorm.io/gorm"

	"github.com/customeros/mailstack/internal/utils"
)

type OrphanEmail struct {
	ID           string    `gorm:"column:id;type:varchar(50);primaryKey"`
	MessageID    string    `gorm:"column:message_id;type:varchar(255);uniqueIndex"`
	ReferencedBy string    `gorm:"column:referenced_by;type:varchar(255)"` // Email ID that referenced this
	ThreadID     string    `gorm:"column:thread_id;type:varchar(50);index"`
	MailboxID    string    `gorm:"column:mailbox_id;type:varchar(50);index"`
	CreatedAt    time.Time `gorm:"column:created_at;type:timestamp;default:current_timestamp"`
}

func (OrphanEmail) TableName() string {
	return "orphan_emails"
}

func (m *OrphanEmail) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = utils.GenerateNanoIDWithPrefix("orpn", 12)
	}
	return nil
}
