package dto

import (
	"sort"
	"time"

	"github.com/customeros/mailstack/internal/enum"
)

type EmailRecord struct {
	// Primary identifiers
	ID         string
	MailboxID  string
	MessageID  string
	ThreadID   string
	InReplyTo  string
	References []string

	// Email metadata
	Direction    enum.EmailDirection
	Status       enum.EmailStatus
	StatusDetail string
	Folder       string
	ImapUID      uint32
	EmailHash    string
	EmailKey     string

	// Core email content
	Subject      string
	CleanSubject string
	FromAddress  string
	FromName     string
	FromUser     string
	FromDomain   string
	ReplyTo      string
	ToAddresses  []string
	CcAddresses  []string
	BccAddresses []string

	// Content fields
	BodyText      string
	BodyHTML      string
	BodyMarkdown  string
	HasAttachment bool
	HasSignature  bool

	// Engagement metrics
	TrackClicks bool

	// Classification
	Classification       enum.EmailClassification
	ClassificationReason string

	// Time information
	SentAt        *time.Time
	ReceivedAt    *time.Time
	LastAttemptAt *time.Time
	ScheduledFor  *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (e *EmailRecord) Recipients() []string {
	uniqueMap := make(map[string]struct{})

	for _, addr := range e.ToAddresses {
		uniqueMap[addr] = struct{}{}
	}

	for _, addr := range e.CcAddresses {
		uniqueMap[addr] = struct{}{}
	}

	for _, addr := range e.BccAddresses {
		uniqueMap[addr] = struct{}{}
	}

	uniqueRecipients := make([]string, 0, len(uniqueMap))
	for addr := range uniqueMap {
		uniqueRecipients = append(uniqueRecipients, addr)
	}

	sort.Strings(uniqueRecipients)
	return uniqueRecipients
}

// TODO
func (e *EmailRecord) HasRichContent() bool {
	return false
}

// TODO
func (e *EmailRecord) BuildHeaders() map[string]string {
	return nil
}

// TODO
func (e *EmailRecord) AllParticipants() []string {
	return nil
}
