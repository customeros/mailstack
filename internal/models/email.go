package models

import (
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/utils"
)

// Email represents a raw email message stored in the database
type Email struct {
	ID        string              `gorm:"column:id;type:varchar(50);primaryKey" json:"id"`
	MailboxID string              `gorm:"column:mailbox_id;type:varchar(50);index;not null" json:"mailboxId"`
	Direction enum.EmailDirection `gorm:"column:direction;type:varchar(20);index;not null" json:"direction"`
	Status    enum.EmailStatus    `gorm:"column:status;type:varchar(20);index" json:"status"`
	MessageID string              `gorm:"column:message_id;uniqueIndex;type:varchar(255);not null" json:"messageId"`
	ThreadID  string              `gorm:"column:thread_id;type:varchar(255);index" json:"threadId"`

	// Core email metadata
	Subject      string         `gorm:"column:subject;type:varchar(1000)" json:"subject"`
	FromAddress  string         `gorm:"column:from_address;type:varchar(255);index" json:"fromAddress"`
	FromName     string         `gorm:"column:from_name;type:varchar(255)" json:"fromName"`
	FromUser     string         `gorm:"column:from_user;type:varchar(255)" json:"fromUser"`
	FromDomain   string         `gorm:"column:from_domain;type:varchar(255)" json:"fromDomain"`
	ReplyTo      string         `gorm:"column:reply_to;type:varchar(255);index" json:"replyTo"`
	ToAddresses  pq.StringArray `gorm:"column:to_addresses;type:text[]" json:"toAddresses"`
	CcAddresses  pq.StringArray `gorm:"column:cc_addresses;type:text[]" json:"ccAddresses"`
	BccAddresses pq.StringArray `gorm:"column:bcc_addresses;type:text[]" json:"bccAddresses"`
	TrackClicks  bool           `gorm:"column:track_clicks;default:false" json:"trackClicks"`
	IsViewed     bool           `gorm:"column:isViewed;default:false" json:"isViewed"`

	Folder       string         `gorm:"column:folder;type:varchar(100);not null" json:"folder"`

	// Content
	Body          string `gorm:"column:body;type:text" json:"body"`
	HasAttachment bool   `gorm:"column:has_attachment;default:false" json:"hasAttachment"`

	// Send Details
	StatusDetail string `gorm:"column:status_detail;type:text" json:"statusDetail"` // Error message or delivery info
	SendAttempts int    `gorm:"column:send_attempts;default:0" json:"sendAttempts"` // Number of send attempts

	// Time information
	SentAt        *time.Time `gorm:"column:sent_at;type:timestamp;index" json:"sentAt"`
	ReceivedAt    *time.Time `gorm:"column:received_at;type:timestamp;index" json:"receivedAt"`
	LastAttemptAt *time.Time `gorm:"column:last_attempt_at;type:timestamp" json:"lastAttemptAt"`    // When last send attempt occurred
	ScheduledFor  *time.Time `gorm:"column:scheduled_for;type:timestamp;index" json:"scheduledFor"` // For scheduled sends

	// Standard timestamps
	CreatedAt time.Time `gorm:"column:created_at;type:timestamp;default:current_timestamp" json:"createdAt"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:timestamp;default:current_timestamp" json:"updatedAt"`
}

func (Email) TableName() string {
	return "emails"
}

func (e *Email) BeforeCreate(tx *gorm.DB) error {
	if e.ID == "" {
		e.ID = utils.GenerateNanoIDWithPrefix("email", 21)
	}
	e.CreatedAt = utils.Now()
	return nil
}

func (e *Email) AllRecipients() []string {
	// Pre-allocate slice with enough capacity
	recipients := make([]string, 0, len(e.ToAddresses)+len(e.CcAddresses)+len(e.BccAddresses))

	recipients = append(recipients, e.ToAddresses...)
	recipients = append(recipients, e.CcAddresses...)
	recipients = append(recipients, e.BccAddresses...)

	return utils.UniqueEmails(recipients)
}
