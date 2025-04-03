package dto

import "github.com/customeros/mailstack/internal/enum"

type AttachToThreadRequest struct {
	EmailID    string   `json:"emailId"`
	MessageID  string   `json:"messageId"`
	InReplyTo  string   `json:"inReplyTo"`
	References []string `json:"references"`
}

type AttachToThreadResponse struct {
	EmailID   string `json:"emailId"`
	MessageID string `json:"messageId"`
	ThreadID  string `json:"threadId"`
}

func (e AttachToThreadRequest) EventType() enum.EmailEvent {
	return enum.EventEmailInboundThread
}
