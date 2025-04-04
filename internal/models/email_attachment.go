package models

import (
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"github.com/customeros/mailstack/internal/utils"
)

// EmailAttachment represents an attachment to an email
type EmailAttachment struct {
	ID          string         `gorm:"column:id;type:varchar(50);primaryKey"`
	EmailIDs    pq.StringArray `gorm:"column:email_ids;type:varchar(50)[];index;not null"`
	Filename    string         `gorm:"column:filename;type:varchar(500)"`
	ContentType string         `gorm:"column:content_type;type:varchar(255)"`
	ContentID   string         `gorm:"column:content_id;type:varchar(255)"` // For inline attachments
	Size        int            `gorm:"column:size;default:0"`
	IsInline    bool           `gorm:"column:is_inline;default:false"`

	// Storage options
	StorageService string `gorm:"column:storage_service;type:varchar(50)"` // "s3", "azure", "local", etc.
	StorageBucket  string `gorm:"column:storage_bucket;type:varchar(255)"` // For cloud storage
	StorageKey     string `gorm:"column:storage_key;type:varchar(1000)"`   // If stored in S3/blob storage

	// Security and verification
	ContentHash string `gorm:"column:content_hash;type:varchar(64);index"` // SHA-256 hash of content

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
