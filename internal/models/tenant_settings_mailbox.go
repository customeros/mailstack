package models

import "time"

type TenantSettingsMailbox struct {
	ID        string    `gorm:"primary_key;type:uuid;default:gen_random_uuid()" json:"id"`
	Tenant    string    `gorm:"column:tenant;type:varchar(255);NOT NULL" json:"tenant"`
	CreatedAt time.Time `gorm:"column:created_at;type:timestamp;DEFAULT:current_timestamp" json:"createdAt"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:timestamp" json:"updatedAt"`

	MailboxUsername string `gorm:"column:mailbox_username;type:varchar(255)" json:"mailboxUsername"`
	MailboxPassword string `gorm:"column:mailbox_password;type:varchar(255)" json:"mailboxPassword"`

	Domain   string `gorm:"column:domain;type:varchar(255)" json:"domain"`
	Username string `gorm:"column:user_name;type:varchar(255)" json:"userName"`

	UserId string `gorm:"column:user_id;type:varchar(255)" json:"userId"` // linked user in neo4j

	ForwardingTo   string `gorm:"column:forwarding_to;type:text" json:"forwardingTo"`
	WebmailEnabled bool   `gorm:"column:webmail_enabled;type:boolean" json:"webmailEnabled"`

	LastRampUpAt  time.Time `gorm:"column:last_ramp_up_at;type:timestamp" json:"lastRampUpAt"`
	RampUpRate    int       `gorm:"type:integer" json:"rampUpRate"`
	RampUpMax     int       `gorm:"type:integer" json:"rampUpMax"`
	RampUpCurrent int       `gorm:"type:integer" json:"rampUpCurrent"`

	MinMinutesBetweenEmails int `gorm:"type:integer" json:"minMinutesBetweenEmails"`
	MaxMinutesBetweenEmails int `gorm:"type:integer" json:"maxMinutesBetweenEmails"`

	ConfigureAttemptAt *time.Time    `gorm:"column:configure_attempt_at;type:timestamp" json:"configureAttemptAt"`
	Status             MailboxStatus `gorm:"column:status;type:varchar(255)" json:"status"`
}

func (TenantSettingsMailbox) TableName() string {
	return "tenant_settings_mailbox"
}

type MailboxStatus string

const (
	MailboxStatusPendingProvisioning MailboxStatus = "PENDING_PROVISIONING"
	MailboxStatusProvisioned         MailboxStatus = "PROVISIONED"
)
