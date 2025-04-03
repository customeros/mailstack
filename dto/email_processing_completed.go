package dto

import "github.com/customeros/mailstack/internal/enum"

type InboundEmailProcessingCompleted struct {
	EmailID string `json:"emailId"`
}

func (e InboundEmailProcessingCompleted) EventType() enum.EmailEvent {
	return enum.EventEmailInboundCompleted
}
