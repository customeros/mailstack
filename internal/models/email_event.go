package models

import (
	"time"

	"gorm.io/gorm"

	"github.com/customeros/mailstack/internal/enum"
)

// EmailEvent represents the main email events table
type EmailEvent struct {
	ID           string                `gorm:"column:id;type:varchar(50);primaryKey;not null" json:"id"`
	Event        enum.EmailEvent       `gorm:"column:event;type:varchar(50);index;not null" json:"event"`
	Publisher    enum.MailstackService `gorm:"column:publisher;type:varchar(50);index;not null" json:"publisher"`
	Timestamp    time.Time             `gorm:"not null;index"`
	Tenant       string                `gorm:"column:tenant;type:varchar(50);index;not null" json:"tenant"`
	User         string                `gorm:"column:user;type:varchar(50);index;not null" json:"user"`
	EmailID      string                `gorm:"column:email_id;type:varchar(50);index;not null" json:"emailId"`
	MailboxID    string                `gorm:"column:mailbox_id;type:varchar(50);index;not null" json:"mailboxId"`
	MessageID    string                `gorm:"column:message_id;type:text;not null;index"` // Changed from uniqueIndex to index
	ThreadID     string                `gorm:"column:thread_id;type:varchar(255);index" json:"threadId"`
	Direction    enum.EmailDirection   `gorm:"column:direction;type:text;not null" json:"direction"`
	Payload      []byte                `gorm:"column:payload;type:bytea" json:"-"`
	HasError     bool                  `gorm:"column:has_error;type:boolean" json:"hasError"`
	ErrorMessage string                `gorm:"column:error_message;type:varchar(255)" json:"errorMessage"`
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
func SetupTimescaleDB(db *gorm.DB) error {
	// Migrate the schema
	if err := db.AutoMigrate(&EmailEvent{}); err != nil {
		return err
	}

	// Convert to hypertable - this only needs to be done once
	if err := db.Exec(`SELECT create_hypertable('email_events', 'timestamp', 
		chunk_time_interval => INTERVAL '1 week',
		if_not_exists => TRUE)`).Error; err != nil {
		return err
	}

	// Create continuous aggregate for daily email statistics
	if err := db.Exec(`
		CREATE MATERIALIZED VIEW IF NOT EXISTS daily_email_stats
		WITH (timescaledb.continuous) AS
		SELECT
			time_bucket('1 day', timestamp) AS day,
			tenant,
			publisher,
			event,
			mailbox_id,
			direction,
			has_error,
			count(*) AS email_count
		FROM email_events
		GROUP BY day, tenant, publisher, event, mailbox_id, direction, has_error
	`).Error; err != nil {
		return err
	}

	// Add refresh policy for continuous aggregate
	if err := db.Exec(`
		SELECT add_continuous_aggregate_policy('daily_email_stats',
			start_offset => INTERVAL '2 days',
			end_offset => INTERVAL '1 hour',
			schedule_interval => INTERVAL '1 day',
			if_not_exists => TRUE)
	`).Error; err != nil {
		return err
	}

	// Create additional indexes for common query patterns
	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_email_events_has_error_timestamp ON email_events (has_error, timestamp DESC);
		CREATE INDEX IF NOT EXISTS idx_email_events_publisher_event ON email_events (publisher, event);
	`).Error; err != nil {
		return err
	}

	return nil
}
