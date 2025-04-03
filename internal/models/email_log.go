package models

import (
	"time"

	"github.com/lib/pq"

	"github.com/customeros/mailstack/internal/enum"
)

type EmailLog struct {
	// Primary identifiers
	ID        string `gorm:"primaryKey;column:id;index" json:"id"`
	MailboxID string `gorm:"column:mailbox_id;index" json:"mailboxId"`
	MessageID string `gorm:"column:message_id;uniqueIndex" json:"messageId"`
	ThreadID  string `gorm:"column:thread_id;index" json:"threadId"`

	// Email metadata
	Direction    enum.EmailDirection `gorm:"column:direction;type:varchar(10)" json:"direction"`
	Status       enum.EmailStatus    `gorm:"column:status;type:varchar(20)" json:"status"`
	StatusDetail string              `gorm:"column:status_detail" json:"statusDetail"`
	EmailHash    string              `gorm:"column:email_hash;index" json:"emailHash"`
	EMLKey       string              `gorm:"column:eml_key" json:"emlKey"`

	// Core email content
	Subject       string         `gorm:"column:subject;type:text" json:"subject"`
	CleanSubject  string         `gorm:"column:clean_subject;type:text" json:"cleanSubject"`
	FromAddress   string         `gorm:"column:from_address" json:"fromAddress"`
	FromName      string         `gorm:"column:from_name" json:"fromName"`
	FromUser      string         `gorm:"column:from_user" json:"fromUser"`
	FromDomain    string         `gorm:"column:from_domain" json:"fromDomain"`
	ReplyTo       string         `gorm:"column:reply_to" json:"replyTo"`
	ToAddresses   pq.StringArray `gorm:"column:to_addresses;type:json" json:"toAddresses"`
	CcAddresses   pq.StringArray `gorm:"column:cc_addresses;type:json" json:"ccAddresses"`
	BccAddresses  pq.StringArray `gorm:"column:bcc_addresses;type:json" json:"bccAddresses"`
	AttachmentIDs pq.StringArray `gorm:"column:attachment_ids;type:json" json:"attachments"`

	// Content fields
	BodyText      string `gorm:"column:body_text;type:text" json:"bodyText"`
	BodyHtml      string `gorm:"column:body_html;type:text" json:"bodyHtml"`
	BodyMarkdown  string `gorm:"column:body_markdown;type:text" json:"bodyMarkdown"`
	HasAttachment bool   `gorm:"column:has_attachment" json:"hasAttachment"`
	HasSignature  bool   `gorm:"column:has_signature" json:"hasSignature"`

	// Engagement metrics
	TrackClicks bool `gorm:"column:track_clicks" json:"trackClicks"`

	// Classification
	Classification       enum.EmailClassification `gorm:"column:classification;type:varchar(30)" json:"classification"`
	ClassificationReason string                   `gorm:"column:classification_reason" json:"classificationReason"`

	// Time information
	SentAt        *time.Time `gorm:"column:sent_at" json:"sentAt,omitempty"`
	ReceivedAt    *time.Time `gorm:"column:received_at" json:"receivedAt,omitempty"`
	LastAttemptAt *time.Time `gorm:"column:last_attempt_at" json:"lastAttemptAt,omitempty"`
	ScheduledFor  *time.Time `gorm:"column:scheduled_for" json:"scheduledFor,omitempty"`
	CreatedAt     time.Time  `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt     time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

// TableName specifies the table name for the EmailRecord model
func (EmailLog) TableName() string {
	return "email_log"
}
