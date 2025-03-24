package models

import (
	"fmt"
	"strings"
	"time"

	"github.com/customeros/mailstack/internal/utils"
)

// EmailStore represents an email message optimized for ClickHouse storage and analytics
type EmailStore struct {
	// Primary identifiers
	ID         string   `ch:"id"`
	MailboxID  string   `ch:"mailbox_id"`
	MessageID  string   `ch:"message_id,pk"` // Using as primary key
	ThreadID   string   `ch:"thread_id"`
	InReplyTo  string   `ch:"in_reply_to"`
	References []string `ch:"references,array"`

	// Email metadata
	Direction    string `ch:"direction"` // Store as string for better compatibility
	Status       string `ch:"status"`
	StatusDetail string `ch:"statusDetail"`
	Folder       string `ch:"folder"`
	ImapUID      uint32 `ch:"imap_uid"`

	// Core email content
	Subject      string   `ch:"subject"`
	CleanSubject string   `ch:"clean_subject"`
	FromAddress  string   `ch:"from_address"`
	FromName     string   `ch:"from_name"`
	FromUser     string   `ch:"from_user"`
	FromDomain   string   `ch:"from_domain"`
	ReplyTo      string   `ch:"reply_to"`
	ToAddresses  []string `ch:"to_addresses,array"`
	CcAddresses  []string `ch:"cc_addresses,array"`
	BccAddresses []string `ch:"bcc_addresses,array"`

	// Simplified content fields
	BodyText      string `ch:"body_text"`
	BodyHTML      string `ch:"body_html"`
	BodyMarkdown  string `ch:"body_markdown"`
	HasAttachment bool   `ch:"has_attachment"`
	HasSignature  bool   `ch:"has_signature"`

	// Engagement metrics
	TrackClicks bool `ch:"track_clicks"`

	// Classification
	Classification       string `ch:"classification"`
	ClassificationReason string `ch:"classification_reason"`

	// Time information - critical for ClickHouse performance
	SentAt        *time.Time `ch:"sent_at,nullable"`
	ReceivedAt    *time.Time `ch:"received_at,nullable"`
	LastAttemptAt *time.Time `ch:"last_attempt_at,nullable"`
	ScheduledFor  *time.Time `ch:"scheduled_for,nullable"`
	CreatedAt     time.Time  `ch:"created_at"`
	UpdatedAt     time.Time  `ch:"updated_at"`

	// Derived fields for partitioning and optimizations
	Date      time.Time `ch:"date"` // Used for partitioning
	YearMonth uint32    `ch:"year_month,materialized:toYYYYMM(date)"`
	DayOfWeek uint8     `ch:"day_of_week,materialized:toDayOfWeek(date)"`
	Hour      uint8     `ch:"hour,materialized:toHour(sent_at)"`

	// Email size metrics
	BodyTextSize uint32 `ch:"body_text_size,materialized:length(body_text)"`
	BodyHTMLSize uint32 `ch:"body_html_size,materialized:length(body_html)"`

	// Special ClickHouse optimization fields
	ShardKey string `ch:"_shard_key,materialized:concat(toString(year_month), message_id)"`
}

// TableName returns the ClickHouse table name
func (EmailStore) TableName() string {
	return "emails"
}

// CreateTableSQL returns the SQL statement to create the emails table
func (EmailStore) CreateTableSQL() string {
	return `
CREATE TABLE IF NOT EXISTS emails (
    id String,
    mailbox_id String,
    message_id String,
    in_reply_to String,
    references Array(String),
    
    direction String,
    status String,
    folder String,
    imap_uid UInt32,
    
    subject String,
    clean_subject String,
    from_address String,
    from_name String,
    from_user String,
    from_domain String,
    reply_to String,
    to_addresses Array(String),
    cc_addresses Array(String),
    bcc_addresses Array(String),
    
    body_text String,
    body_html String,
    has_attachment Bool,
    
    track_clicks Bool,
    
    classification String,
    classification_reason String,
    
    sent_at Nullable(DateTime),
    received_at Nullable(DateTime),
    last_attempt_at Nullable(DateTime),
    scheduled_for Nullable(DateTime),
    created_at DateTime,
    updated_at DateTime,
    
    date Date DEFAULT if(sent_at != 0, toDate(sent_at), toDate(received_at)),
    year_month UInt32 MATERIALIZED toYYYYMM(date),
    day_of_week UInt8 MATERIALIZED toDayOfWeek(date),
    hour UInt8 MATERIALIZED if(sent_at != 0, toHour(sent_at), 0),
    
    body_text_size UInt32 MATERIALIZED length(body_text),
    body_html_size UInt32 MATERIALIZED length(body_html),
    
    _shard_key String MATERIALIZED concat(toString(year_month), message_id)
) ENGINE = MergeTree()
PARTITION BY year_month
ORDER BY (_shard_key)
SETTINGS index_granularity = 8192
`
}

func (e *EmailStore) AllRecipients() []string {
	// Pre-allocate slice with enough capacity
	recipients := make([]string, 0, len(e.ToAddresses)+len(e.CcAddresses)+len(e.BccAddresses))

	recipients = append(recipients, e.ToAddresses...)
	recipients = append(recipients, e.CcAddresses...)
	recipients = append(recipients, e.BccAddresses...)

	return utils.UniqueEmails(recipients)
}

func (e *EmailStore) AllParticipants() []string {
	// Get all recipients
	participants := e.AllRecipients()

	// Add sender (FromAddress) if not empty
	if e.FromAddress != "" {
		participants = append(participants, e.FromAddress)
	}

	return utils.UniqueEmails(participants)
}

type EmailHeaders struct {
	AutoSubmitted      bool
	ContentDescription string
	DeliveryStatus     bool
	ListUnsubscribe    bool
	Precedence         string
	ReturnPath         string
	ReturnPathExists   bool
	XAutoreply         string
	XAutoresponse      string
	XLoop              bool
	XFailedRecipients  []string
	ReplyTo            string
	ReplyToExists      bool
	Sender             string
	ForwardedFor       string
	DKIM               []string
	SPF                string
	DMARC              string
}

func (e *EmailStore) Headers() (*EmailHeaders, error) {
	headers := &EmailHeaders{}

	if e.RawHeaders == nil {
		return headers, nil
	}

	// Helper function to get header value as string
	getString := func(key string) string {
		if values, ok := e.RawHeaders[key].([]string); ok && len(values) > 0 {
			return values[0]
		}
		if value, ok := e.RawHeaders[key].(string); ok {
			return value
		}
		return ""
	}

	// Helper function to check if a header exists
	headerExists := func(key string) bool {
		_, exists := e.RawHeaders[key]
		return exists
	}

	// Helper to get string array
	getStringArray := func(key string) []string {
		if values, ok := e.RawHeaders[key].([]string); ok {
			return values
		}
		if value, ok := e.RawHeaders[key].(string); ok {
			return []string{value}
		}
		return nil
	}

	// Process boolean headers (presence/absence or specific values)
	autoSubmitted := getString("Auto-Submitted")
	headers.AutoSubmitted = autoSubmitted != "" && autoSubmitted != "no"

	// Content-Description
	headers.ContentDescription = getString("Content-Description")

	// Delivery-Status
	headers.DeliveryStatus = headerExists("Delivery-Status") ||
		headerExists("X-Failed-Recipients") ||
		strings.Contains(e.Subject, "Delivery Status Notification") ||
		strings.Contains(e.Subject, "Mail Delivery Failure")

	// List-Unsubscribe
	headers.ListUnsubscribe = headerExists("List-Unsubscribe")

	// Precedence
	headers.Precedence = getString("Precedence")

	// Return-Path
	returnPath := getString("Return-Path")
	headers.ReturnPath = returnPath
	headers.ReturnPathExists = headerExists("Return-Path")

	// Auto-reply headers
	headers.XAutoreply = getString("X-Autoreply")
	headers.XAutoresponse = getString("X-Autoresponse")

	// X-Loop
	headers.XLoop = headerExists("X-Loop")

	// X-Failed-Recipients
	failedRecipientsStr := getString("X-Failed-Recipients")
	if failedRecipientsStr != "" {
		recipients := strings.Split(failedRecipientsStr, ",")
		for i, recipient := range recipients {
			recipients[i] = strings.TrimSpace(recipient)
		}
		headers.XFailedRecipients = recipients
	}

	// Reply-To
	headers.ReplyTo = getString("Reply-To")
	headers.ReplyToExists = headerExists("Reply-To")

	// Sender
	headers.Sender = getString("Sender")

	// Forwarded-For (could be in different formats)
	headers.ForwardedFor = getString("X-Forwarded-For")
	if headers.ForwardedFor == "" {
		headers.ForwardedFor = getString("Forwarded-For")
	}

	// Security headers
	headers.DKIM = getStringArray("DKIM-Signature")
	headers.SPF = getString("Received-SPF")
	headers.DMARC = getString("DMARC-Result")

	return headers, nil
}

// BuildHeaders creates a map of headers for an outgoing email
func (e *EmailStore) BuildHeaders() map[string]string {
	header := make(map[string]string)

	// Build "From" with name if available
	if e.FromName != "" {
		header["From"] = fmt.Sprintf("%s <%s>", e.FromName, e.FromAddress)
	} else {
		header["From"] = e.FromAddress
	}

	header["To"] = strings.Join(e.ToAddresses, ", ")

	if len(e.CcAddresses) > 0 {
		header["Cc"] = strings.Join(e.CcAddresses, ", ")
	}

	header["Subject"] = e.Subject
	header["MIME-Version"] = "1.0"

	// Date header (required by RFC 5322)
	header["Date"] = time.Now().Format(time.RFC1123Z)

	// Set Message-ID
	if e.MessageID != "" {
		if !strings.HasPrefix(e.MessageID, "<") {
			e.MessageID = fmt.Sprintf("<%s>", e.MessageID)
		}
		header["Message-ID"] = e.MessageID
	}

	// Reply-To if different from From
	if e.ReplyTo != "" && e.ReplyTo != e.FromAddress {
		header["Reply-To"] = e.ReplyTo
	}

	// Return-Path (should match From address)
	header["Return-Path"] = fmt.Sprintf("<%s>", e.FromAddress)

	// Set In-Reply-To and References if this is a reply
	if e.InReplyTo != "" {
		header["In-Reply-To"] = e.InReplyTo

		// Build References header
		// RFC 5322 recommends including the original message ID in the references
		references := e.References
		if len(references) == 0 {
			references = pq.StringArray{e.InReplyTo}
		}
		header["References"] = strings.Join(references, " ")
	}

	// Set Reply-To if specified
	if e.ReplyTo != "" {
		header["Reply-To"] = e.ReplyTo
	}

	// X-Mailer helps identify your system
	header["X-Mailer"] = "CustomerOS Mailstack"

	// Add custom headers from RawHeaders if any
	if e.RawHeaders != nil {
		for k, v := range e.RawHeaders {
			// Skip headers we've already set
			if _, exists := header[k]; !exists {
				// Handle different value types (string or []string)
				switch value := v.(type) {
				case string:
					header[k] = value
				case []string:
					if len(value) > 0 {
						header[k] = strings.Join(value, ", ")
					}
				}
			}
		}
	}

	return header
}

func (e *EmailStore) HasRichContent() bool {
	return e.BodyHTML != "" || e.HasAttachment
}
