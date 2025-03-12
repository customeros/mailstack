package models

import (
	"time"

	"gorm.io/gorm"

	"github.com/customeros/mailstack/internal/utils"
)

// EmailAttachment represents an attachment to an email
type EmailAttachment struct {
	ID          string `gorm:"type:varchar(50);primaryKey"`
	EmailID     string `gorm:"type:varchar(50);index;not null"`
	Direction   string `gorm:"type:varchar(10);index;not null"` // "inbound" or "outbound"
	Filename    string `gorm:"type:varchar(500)"`
	ContentType string `gorm:"type:varchar(255)"`
	ContentID   string `gorm:"type:varchar(255)"` // For inline attachments
	Size        int    `gorm:"default:0"`
	IsInline    bool   `gorm:"default:false"`

	// Storage options
	StorageService string `gorm:"type:varchar(50)"`   // "s3", "azure", "local", etc.
	StorageBucket  string `gorm:"type:varchar(255)"`  // For cloud storage
	StorageKey     string `gorm:"type:varchar(1000)"` // If stored in S3/blob storage

	// Additional fields for inbound attachments
	ContentDisposition string `gorm:"type:varchar(100)"` // attachment, inline, etc.
	EncodingType       string `gorm:"type:varchar(50)"`  // base64, quoted-printable, etc.

	// Security and verification
	ContentHash   string `gorm:"type:varchar(64)"` // SHA-256 hash of content
	ScanStatus    string `gorm:"type:varchar(20)"` // clean, infected, pending, etc.
	ScanTimestamp *time.Time

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
		e.ID = utils.GenerateNanoIDWithPrefix("atch", 21)
	}
	e.CreatedAt = utils.Now()
	return nil
}
