package interfaces

import (
	"context"

	"github.com/customeros/mailstack/dto"
	"github.com/customeros/mailstack/internal/models"
)

type EmailProcessor interface {
	NewInboundEmail() *models.Email
	NewAttachment() *models.EmailAttachment
	NewAttachmentFile(attachmentID string, data []byte) *AttachmentFile

	ProcessEmail(ctx context.Context, email *models.Email, attachments []*models.EmailAttachment, files []*AttachmentFile) error
	EmailFilter(ctx context.Context, email *models.Email) error
}

type IMAPProcessor interface {
	EmailProcessor
	ProcessIMAPMessage(ctx context.Context, inboundEmail dto.EmailReceived) error
}

type AttachmentFile struct {
	ID   string
	Data []byte
}
