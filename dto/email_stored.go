package dto

import (
	"github.com/customeros/mailstack/internal/enum"
)

type EmailStored struct {
	ID string `json:"Id"`
}

func (e EmailStored) EventType() enum.EmailEvent {
	return enum.EventEmailInboundStored
}
