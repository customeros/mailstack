// models/mailbox.go
package models

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"github.com/pkg/errors"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/utils"
)

const (
	MAILBOX_IMAP_PORT     = 993
	MAILBOX_IMAP_SERVER   = "mail.hostedemail.com"
	MAILBOX_IMAP_SECURITY = enum.EmailSecurityTLS

	MAILBOX_SMTP_PORT     = 587
	MAILBOX_SMTP_SERVER   = "mail.hostedemail.com"
	MAILBOX_SMTP_SECURITY = enum.EmailSecurityTLS

	MAILBOX_INBOX = "INBOX"
	MAILBOX_SENT  = "Sent"
	MAILBOX_SPAM  = "Spam"

	MAILBOX_GOOGLE_IMAP_SERVER   = "imap.gmail.com"
	MAILBOX_GOOGLE_IMAP_PORT     = 993
	MAILBOX_GOOGLE_IMAP_SECURITY = enum.EmailSecurityTLS
	MAILBOX_GOOGLE_INBOX         = "INBOX"
	MAILBOX_GOOGLE_SENT          = "[Gmail]/Sent Mail"
	MAILBOX_GOOGLE_SPAM          = "[Gmail]/Spam"
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
	SenderID      string             `gorm:"column:sender_id;type:varchar(255);index" json:"senderId"`

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
	OAuthScope        string     `gorm:"column:oauth_scope;type:varchar(2000)" json:"oauthScope"`
	OAuthTokenId      string     `gorm:"column:oauth_token_id;type:varchar(2000)" json:"oauthTokenId"`

	// Email sending configuration
	ReplyToAddress string `gorm:"column:reply_to_address;type:varchar(255)" json:"replyToAddress"`

	// Sync configuration
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

	// Fields from previous mailbox model
	ProvisionStatus         MailboxProvisionStatus `gorm:"column:provision_status;type:varchar(255)" json:"provisionStatus"`
	MinMinutesBetweenEmails int                    `gorm:"type:integer" json:"minMinutesBetweenEmails"`
	MaxMinutesBetweenEmails int                    `gorm:"type:integer" json:"maxMinutesBetweenEmails"`
	ConfigureAttemptAt      *time.Time             `gorm:"column:configure_attempt_at;type:timestamp" json:"configureAttemptAt"`
	LastRampUpAt            time.Time              `gorm:"column:last_ramp_up_at;type:timestamp" json:"lastRampUpAt"`
	RampUpRate              int                    `gorm:"type:integer" json:"rampUpRate"`
	RampUpMax               int                    `gorm:"type:integer" json:"rampUpMax"`
	RampUpCurrent           int                    `gorm:"type:integer" json:"rampUpCurrent"`
	ForwardingTo            string                 `gorm:"column:forwarding_to;type:text" json:"forwardingTo"`
	WebmailEnabled          bool                   `gorm:"column:webmail_enabled;type:boolean" json:"webmailEnabled"`
}

// TableName sets the table name for the Mailbox model
func (Mailbox) TableName() string {
	return "mailboxes"
}

func (m *Mailbox) BeforeCreate(*gorm.DB) error {
	if m.ID == "" {
		m.ID = utils.GenerateNanoIDWithPrefix("mbox", 16)
	}
	return nil
}

type MailboxProvisionStatus string

const (
	MailboxStatusPendingProvisioning MailboxProvisionStatus = "PENDING_PROVISIONING"
	MailboxStatusProvisioned         MailboxProvisionStatus = "PROVISIONED"
)

// Encrypt a token using AES-GCM
func EncryptToken(key string, token string) (string, error) {
	decodedKey, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(decodedKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(token), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt a token using AES-GCM
func DecryptToken(key string, encryptedToken string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encryptedToken)
	if err != nil {
		return "", err
	}

	decodedKey, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(decodedKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}
