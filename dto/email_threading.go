package dto

import (
	"time"

	"github.com/customeros/mailstack/internal/enum"
)

type AttachToThreadRequest struct {
	EmailID         string     `json:"emailId"`
	MailboxID       string     `json:"mailboxId"`
	MessageID       string     `json:"messageId"`
	ReplyTo         string     `json:"replyTo"`
	References      []string   `json:"references"`
	Subject         string     `json:"subject"`
	AllParticipants []string   `json:"allParticipants"`
	EmailSentAt     *time.Time `json:"emailSentAt"`
	EmailReceivedAt *time.Time `json:"emailReceivedAt"`
}

type AttachToThreadResponse struct {
	EmailID      string `json:"emailId"`
	MessageID    string `json:"messageId"`
	ThreadID     string `json:"threadId"`
	ErrorMessage string `json:"errorMessage"`
}

func (e AttachToThreadRequest) EventType() enum.EmailEvent {
	return enum.EventEmailInboundThread
}
