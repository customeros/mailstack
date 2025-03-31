package models

import (
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
)

// EmailEvent represents the main email events table
type EmailEvent struct {
	ID                   int64          `gorm:"primaryKey;autoIncrement"`
	Timestamp            time.Time      `gorm:"not null;index"` // Primary time column for TimescaleDB
	Tenant               string         `gorm:"column:tenant;type:varchar(50);index;not null" json:"tenant"`
	EmailID              string         `gorm:"column:email_id;type:varchar(50);index;not null" json:"emailId"`
	MailboxID            string         `gorm:"column:mailbox_id;type:varchar(50);index;not null" json:"mailboxId"`
	MessageID            string         `gorm:"column:message_id;type:text;not null;uniqueIndex"`
	ThreadID             string         `gorm:"column:thread_id;type:varchar(255);index" json:"threadId"`
	FromAddress          string         `gorm:"column:from_address;type:varchar(255);index" json:"fromAddress"`
	FromUser             string         `gorm:"column:from_user;type:varchar(255)" json:"fromUser"`
	FromDomain           string         `gorm:"column:from_domain;type:varchar(255)" json:"fromDomain"`
	Recipients           pq.StringArray `gorm:"column:recipients;type:text[]" json:"recipients"`
	EmailKey             string         `gorm:"column:email_key;type:text;not null" json:"emailKey"`
	Subject              string         `gorm:"column:subject;type:varchar(1000)" json:"subject"`
	Direction            string         `gorm:"column:direction;type:text;not null;check:direction IN ('inbound', 'outbound')"`
	Classification       string         `gorm:"column:classification;type:varchar(50);index" json:"classification"`
	ClassificationReason string         `gorm:"column:classification_reason;type:varchar(255)" json:"classificationReason"`
	SentAt               *time.Time     `gorm:"column:sent_at;type:timestamp;index" json:"sentAt"`
	ReceivedAt           *time.Time     `gorm:"column:received_at;type:timestamp;index" json:"receivedAt"`
	ScheduledFor         *time.Time     `gorm:"column:scheduled_for;type:timestamp;index" json:"scheduledFor"` // For scheduled sends

	Attachments pq.StringArray `gorm:"column:attachments;type:text[]" json:"attachments"`
}

// TableName overrides the table name
func (EmailEvent) TableName() string {
	return "email_events"
}

// BeforeCreate hook to ensure the timestamp is set
func (e *EmailEvent) BeforeCreate(tx *gorm.DB) error {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}
	return nil
}

// SetupTimescaleDB initializes the TimescaleDB specifics for this model
func (e *EmailEvent) SetupTimescaleDB(db *gorm.DB) error {
	// Migrate the schema
	if err := db.AutoMigrate(&EmailEvent{}); err != nil {
		return err
	}

	// Convert to hypertable - this only needs to be done once
	db.Exec(`SELECT create_hypertable('email_events', 'timestamp', 
		chunk_time_interval => INTERVAL '1 week',
		if_not_exists => TRUE)`)

	// Create continuous aggregate for daily email statistics
	db.Exec(`
		CREATE MATERIALIZED VIEW IF NOT EXISTS daily_email_stats
		WITH (timescaledb.continuous) AS
		SELECT
			time_bucket('1 day', timestamp) AS day,
			tenant,
			mailbox_id,
			direction,
			classification,
			count(*) AS email_count
		FROM email_events
		GROUP BY day, tenant, mailbox_id, direction, classification
	`)

	// Add refresh policy for continuous aggregate
	db.Exec(`
		SELECT add_continuous_aggregate_policy('daily_email_stats',
			start_offset => INTERVAL '2 days',
			end_offset => INTERVAL '1 hour',
			schedule_interval => INTERVAL '1 day',
			if_not_exists => TRUE)
	`)

	return nil
}
