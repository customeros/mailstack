// models/mailbox.go
package models

import (
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/utils"
)

// Mailbox represents an email account configuration with provider-specific settings
type Mailbox struct {
	ID            string             `gorm:"column:id;type:varchar(50);primaryKey" json:"id"`
	Tenant        string             `gorm:"column:tenant;type:varchar(255)" json:"tenant"`
	UserID        string             `gorm:"column:user_id;type:varchar(255);index" json:"userId"`
	Provider      enum.EmailProvider `gorm:"column:provider;type:varchar(50);index;not null" json:"provider"`
	EmailAddress  string             `gorm:"column:email_address;type:varchar(255);index" json:"emailAddress"`
	MailboxUser   string             `gorm:"column:mailbox_user;type:varchar(255);index" json:"mailboxUser"`
	MailboxDomain string             `gorm:"column:mailbox_domain;type:varchar(255);index" json:"mailboxDomain"`

	// Common connection properties
	InboundEnabled  bool `gorm:"column:inbound_enabled;default:true" json:"inboundEnabled"`
	OutboundEnabled bool `gorm:"column:outbound_enabled;default:true" json:"outboundEnabled"`

	// Protocol-specific configurations (null for API-based providers)
	ImapServer   string             `gorm:"column:imap_server;type:varchar(255)" json:"imapServer"`
	ImapPort     int                `gorm:"column:imap_port" json:"imapPort"`
	ImapUsername string             `gorm:"column:imap_username;type:varchar(255)" json:"imapUsername"`
	ImapPassword string             `gorm:"column:imap_password;type:varchar(255)" json:"imapPassword"`
	ImapSecurity enum.EmailSecurity `gorm:"column:imap_security;type:varchar(50)" json:"imapSecurity"`

	SmtpServer   string             `gorm:"column:smtp_server;type:varchar(255)" json:"smtpServer"`
	SmtpPort     int                `gorm:"column:smtp_port" json:"smtpPort"`
	SmtpUsername string             `gorm:"column:smtp_username;type:varchar(255)" json:"smtpUsername"`
	SmtpPassword string             `gorm:"column:smtp_password;type:varchar(255)" json:"smtpPassword"`
	SmtpSecurity enum.EmailSecurity `gorm:"column:smtp_security;type:varchar(50)" json:"smtpSecurity"`

	// OAuth specific fields (for Google, Microsoft, etc.)
	OAuthClientID     string     `gorm:"column:oauth_client_id;type:varchar(255)" json:"oauthClientId"`
	OAuthClientSecret string     `gorm:"column:oauth_client_secret;type:varchar(255)" json:"oauthClientSecret"`
	OAuthRefreshToken string     `gorm:"column:oauth_refresh_token;type:varchar(1000)" json:"oauthRefreshToken"`
	OAuthAccessToken  string     `gorm:"column:oauth_access_token;type:varchar(1000)" json:"oauthAccessToken"`
	OAuthTokenExpiry  *time.Time `gorm:"column:oauth_token_expiry;type:timestamp" json:"oauthTokenExpiry"`
	OAuthScope        string     `gorm:"column:oauth_scope;type:varchar(500)" json:"oauthScope"`

	// Email sending configuration
	DefaultFromName string `gorm:"column:default_from_name;type:varchar(255)" json:"defaultFromName"`
	ReplyToAddress  string `gorm:"column:reply_to_address;type:varchar(255)" json:"replyToAddress"`
	SignatureHTML   string `gorm:"column:signature_html;type:text" json:"signatureHtml"`
	SignaturePlain  string `gorm:"column:signature_plain;type:text" json:"signaturePlain"`

	// Sync configuration
	SyncEnabled bool           `gorm:"column:sync_enabled;default:true" json:"syncEnabled"`
	SyncFolders pq.StringArray `gorm:"column:sync_folders;type:text[]" json:"syncFolders"`

	// Status tracking
	ConnectionStatus    enum.ConnectionStatus `gorm:"column:connection_status;type:varchar(50)" json:"connectionStatus"`
	LastConnectionCheck *time.Time            `gorm:"column:last_connection_check;type:timestamp" json:"lastConnectionCheck"`
	ErrorMessage        string                `gorm:"column:error_message;type:text" json:"errorMessage"`

	// Send rate limits
	DailySendQuota int        `gorm:"column:daily_quota;default:2000" json:"dailyQuota"`
	DailySendCount int        `gorm:"column:daily_send_count;default:0" json:"dailySendCount"`
	QuotaResetAt   *time.Time `gorm:"column:quota_reset_at;type:timestamp" json:"quotaResetAt"`

	// Standard timestamps
	CreatedAt time.Time      `gorm:"column:created_at;type:timestamp;default:current_timestamp" json:"createdAt"`
	UpdatedAt time.Time      `gorm:"column:updated_at;type:timestamp;default:current_timestamp" json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index" json:"-"`
}

// TableName sets the table name for the Mailbox model
func (Mailbox) TableName() string {
	return "mailboxes"
}

func (m *Mailbox) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = utils.GenerateNanoIDWithPrefix("mbox", 16)
	}
	return nil
}
