package models

import (
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"github.com/customeros/mailstack/internal/utils"
)

// EmailAttachment represents an attachment to an email
type EmailAttachment struct {
	ID          string         `gorm:"type:varchar(50);primaryKey"`
	Emails      pq.StringArray `gorm:"type:varchar(50)[];index;not null"`
	Threads     pq.StringArray `gorm:"type:varchar(50)[];index;not null"`
	Filename    string         `gorm:"type:varchar(500)"`
	ContentType string         `gorm:"type:varchar(255)"`
	ContentID   string         `gorm:"type:varchar(255)"` // For inline attachments
	Size        int            `gorm:"default:0"`
	IsInline    bool           `gorm:"default:false"`

	// Storage options
	StorageService string `gorm:"type:varchar(50)"`   // "s3", "azure", "local", etc.
	StorageBucket  string `gorm:"type:varchar(255)"`  // For cloud storage
	StorageKey     string `gorm:"type:varchar(1000)"` // If stored in S3/blob storage

	// Security and verification
	ContentHash string `gorm:"type:varchar(64);index"` // SHA-256 hash of content

	// Standard timestamps
	CreatedAt time.Time `gorm:"column:created_at;type:timestamp;default:current_timestamp" json:"createdAt"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:timestamp;default:current_timestamp" json:"updatedAt"`
}

// TableName overrides the table name for EmailAttachment
func (EmailAttachment) TableName() string {
	return "email_attachments"
}

func (e *EmailAttachment) BeforeCreate(tx *gorm.DB) error {
	if e.ID == "" {
		e.ID = utils.GenerateNanoIDWithPrefix("file", 12)
	}
	e.CreatedAt = utils.Now()
	return nil
}
