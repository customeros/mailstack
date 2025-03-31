package interfaces

import (
	"context"

	"github.com/emersion/go-imap"

	"github.com/customeros/mailstack/dto"
	"github.com/customeros/mailstack/internal/models"
)

type EmailProcessor interface {
	NewInboundEmail() *dto.EmailRecord
	NewAttachment() *models.EmailAttachment
	NewAttachmentFile(attachmentID string, data []byte) *AttachmentFile

	ProcessEmail(ctx context.Context, email *dto.EmailRecord, attachments []*models.EmailAttachment, files []*AttachmentFile) error
	EmailFilter(ctx context.Context, email *dto.EmailRecord, headers map[string]interface{}) error
}

type IMAPProcessor interface {
	EmailProcessor
	ProcessIMAPMessage(ctx context.Context, inboundEmail dto.EmailReceived) error
	SaveMessageAsEML(ctx context.Context, msg *imap.Message, emailID string) (string, error)
}

type AttachmentFile struct {
	ID   string
	Data []byte
}
