package models

import (
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/utils"
)

// Email represents a raw email message stored in the database
type Email struct {
	ID         string              `gorm:"column:id;type:varchar(50);primaryKey" json:"id"`
	MailboxID  string              `gorm:"column:mailbox_id;type:varchar(50);index;not null" json:"mailboxId"`
	Direction  enum.EmailDirection `gorm:"column:direction;type:varchar(20);index;not null" json:"direction"`
	Status     enum.EmailStatus    `gorm:"column:status;type:varchar(20);index" json:"status"`
	Folder     string              `gorm:"column:folder;type:varchar(100);index;not null" json:"folder"`
	ImapUID    uint32              `gorm:"column:imap_uid;index" json:"imapUid"`
	MessageID  string              `gorm:"column:message_id;uniqueIndex;type:varchar(255);not null" json:"messageId"`
	ThreadID   string              `gorm:"column:thread_id;type:varchar(255);index" json:"threadId"`
	InReplyTo  string              `gorm:"column:in_reply_to;type:varchar(255);index" json:"inReplyTo"`
	References pq.StringArray      `gorm:"column:references;type:text[]" json:"references"`

	// Core email metadata
	Subject      string         `gorm:"column:subject;type:varchar(1000)" json:"subject"`
	CleanSubject string         `gorm:"column:clean_subject;type:varchar(1000)" json:"cleanSubject"`
	FromAddress  string         `gorm:"column:from_address;type:varchar(255);index" json:"fromAddress"`
	FromName     string         `gorm:"column:from_name;type:varchar(255)" json:"fromName"`
	FromUser     string         `gorm:"column:from_user;type:varchar(255)" json:"fromUser"`
	FromDomain   string         `gorm:"column:from_domain;type:varchar(255)" json:"fromDomain"`
	ReplyTo      string         `gorm:"column:reply_to;type:varchar(255);index" json:"replyTo"`
	ToAddresses  pq.StringArray `gorm:"column:to_addresses;type:text[]" json:"toAddresses"`
	CcAddresses  pq.StringArray `gorm:"column:cc_addresses;type:text[]" json:"ccAddresses"`
	BccAddresses pq.StringArray `gorm:"column:bcc_addresses;type:text[]" json:"bccAddresses"`
	TrackClicks  bool           `gorm:"column:track_clicks;default:false" json:"trackClicks"`

	// Content
	BodyText      string `gorm:"column:body_text;type:text" json:"bodyText"`
	BodyHTML      string `gorm:"column:body_html;type:text" json:"bodyHtml"`
	BodyMarkdown  string `gorm:"column:body_markdown;type:text" json:"bodyMarkdown"`
	HasAttachment bool   `gorm:"column:has_attachment;default:false" json:"hasAttachment"`
	HasSignature  bool   `gorm:"column:has_signature;default:false" json:"hasSignature"`

	// Send Details
	StatusDetail string `gorm:"column:status_detail;type:text" json:"statusDetail"` // Error message or delivery info
	SendAttempts int    `gorm:"column:send_attempts;default:0" json:"sendAttempts"` // Number of send attempts

	// Time information
	SentAt        *time.Time `gorm:"column:sent_at;type:timestamp;index" json:"sentAt"`
	ReceivedAt    *time.Time `gorm:"column:received_at;type:timestamp;index" json:"receivedAt"`
	LastAttemptAt *time.Time `gorm:"column:last_attempt_at;type:timestamp" json:"lastAttemptAt"`    // When last send attempt occurred
	ScheduledFor  *time.Time `gorm:"column:scheduled_for;type:timestamp;index" json:"scheduledFor"` // For scheduled sends

	// Extensions and provider-specific data
	GmailLabels       pq.StringArray `gorm:"column:gmail_labels;type:text[]" json:"gmailLabels"`
	OutlookCategories pq.StringArray `gorm:"column:outlook_categories;type:text[]" json:"outlookCategories"`
	MailstackFlags    pq.StringArray `gorm:"column:mailstack_flags;type:text[]" json:"mailstackFlags"`

	// Raw data
	RawHeaders    JSONMap `gorm:"column:raw_headers;type:jsonb" json:"rawHeaders"`
	Envelope      JSONMap `gorm:"column:envelope;type:jsonb" json:"envelope"`
	BodyStructure JSONMap `gorm:"column:body_structure;type:jsonb" json:"bodyStructure"`

	// Classification
	Classification       enum.EmailClassification `gorm:"column:classification;type:varchar(50);index" json:"classification"`
	ClassificationReason string                   `gorm:"column:classification_reason;type:text" json:"classificationReason"`

	// Standard timestamps
	CreatedAt time.Time `gorm:"column:created_at;type:timestamp;default:current_timestamp" json:"createdAt"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:timestamp;default:current_timestamp" json:"updatedAt"`
}

func (Email) TableName() string {
	return "emails"
}

func (e *Email) BeforeCreate(tx *gorm.DB) error {
	if e.ID == "" {
		e.ID = utils.GenerateNanoIDWithPrefix("email", 24)
	}
	e.CreatedAt = utils.Now()
	return nil
}

// EmailHeaders represents specific email header information needed for processing
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

func (e *Email) Headers() (*EmailHeaders, error) {
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
func (e *Email) BuildHeaders() map[string]string {
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

func (e *Email) AllRecipients() []string {
	// Pre-allocate slice with enough capacity
	recipients := make([]string, 0, len(e.ToAddresses)+len(e.CcAddresses)+len(e.BccAddresses))

	recipients = append(recipients, e.ToAddresses...)
	recipients = append(recipients, e.CcAddresses...)
	recipients = append(recipients, e.BccAddresses...)

	return utils.UniqueEmails(recipients)
}

// AllParticipants returns all email participants including sender and recipients
func (e *Email) AllParticipants() []string {
	// Get all recipients
	participants := e.AllRecipients()

	// Add sender (FromAddress) if not empty
	if e.FromAddress != "" {
		participants = append(participants, e.FromAddress)
	}

	return utils.UniqueEmails(participants)
}

func (e *Email) HasRichContent() bool {
	return e.BodyHTML != "" || e.HasAttachment
}
