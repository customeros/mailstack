package dto

import "github.com/customeros/mailstack/internal/enum"

type ProcessAttachmentRequest struct {
	EmailID     string               `json:"emailId"`
	Attachments []AttachmentMetadata `json:"attachments"`
}

type ProcessAttachmentResponse struct {
	EmailID       string   `json:"emailId"`
	HasAttachment bool     `json:"hasAttachment"`
	AttachmentIDs []string `json:"attachmentIds"`
}

type AttachmentMetadata struct {
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	ContentID   string `json:"contentId"`
	Size        int    `json:"size"`
	IsInline    bool   `json:"isInline"`
	StorageKey  string `json:"storageKey"`
	ObjectInfo  string `json:"objectInfo"`
}

func (e ProcessAttachmentRequest) EventType() enum.EmailEvent {
	return enum.EventEmailInboundAttachments
}
