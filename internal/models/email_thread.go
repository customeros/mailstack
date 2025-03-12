package models

import (
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"github.com/customeros/mailstack/internal/utils"
)

type EmailThread struct {
	ID             string         `gorm:"column:id;type:varchar(50);primaryKey" json:"id"`
	Subject        string         `gorm:"column:subject;type:varchar(1000)" json:"subject"`
	Participants   pq.StringArray `gorm:"column:participants;type:text[]" json:"participants"`
	MessageCount   int            `gorm:"column:message_count;default:0" json:"messageCount"`
	MailboxID      string         `gorm:"column:mailbox_id;type:varchar(50);index" json:"mailboxId"`
	LastMessageID  string         `gorm:"column:last_message_id;type:varchar(50)" json:"lastMessageId"`
	HasAttachments bool           `gorm:"column:has_attachments;default:false" json:"hasAttachments"`
	LastMessageAt  *time.Time     `gorm:"column:last_message_at;type:timestamp" json:"lastMessageAt"`
	FirstMessageAt *time.Time     `gorm:"column:first_message_at;type:timestamp" json:"firstMessageAt"`
	CreatedAt      time.Time      `gorm:"column:created_at;type:timestamp" json:"createdAt"`
	UpdatedAt      time.Time      `gorm:"column:updated_at;type:timestamp" json:"updatedAt"`
}

func (EmailThread) TableName() string {
	return "email_threads"
}

func (e *EmailThread) BeforeCreate(tx *gorm.DB) error {
	if e.ID == "" {
		e.ID = utils.GenerateNanoIDWithPrefix("thread", 16)
	}
	e.CreatedAt = utils.Now()
	return nil
}
