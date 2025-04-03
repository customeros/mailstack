package email_storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/emersion/go-imap"

	"github.com/customeros/mailstack/dto"
	mailstack_errors "github.com/customeros/mailstack/internal/errors"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/telemetry"
	"github.com/customeros/mailstack/internal/utils"
)

// HandleRawEmail processes a single raw email message
func (s *EmailStorageService) handleIMAPEmail(ctx context.Context, event dto.EmailReceivedIMAP, eventRecord *models.EmailEvent) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailStorageService.handleRawEmail")
	defer spans.Finish()
	spans.LogObjectAsJson("emailEvent", event)

	email := s.NewEmailLog()

	email.EmailHash = utils.GenerateIMAPHash(event.MailboxID, event.Folder, event.ImapUID)

	emailExists, err := s.repositories.EmailLogRepository.IsDuplicateByHash(ctx, email.EmailHash)
	if err != nil {
		spans.TraceError(err)
		eventRecord.ErrorMessage = err.Error()
		return
	}
	if emailExists {
		eventRecord.ErrorMessage = mailstack_errors.ErrEmailAlreadyProcessed.Error()
		return
	}

	// get message from imap server
	msg, err := s.imapService.GetMessageByUID(ctx, event.MailboxID, event.Folder, event.ImapUID)
	if err != nil {
		spans.TraceError(err)
		eventRecord.ErrorMessage = err.Error()
		return
	}

	// save message as .eml
	bucketKey, err := s.SaveIMAPMessageAsEML(ctx, msg, email.ID)
	if err != nil {
		spans.TraceError(err)
		eventRecord.ErrorMessage = err.Error()
		return
	}
	email.EMLKey = bucketKey

	// log email
	err = s.repositories.EmailLogRepository.Create(ctx, email)
	if err != nil {
		eventRecord.ErrorMessage = err.Error()
		spans.TraceError(err)
		return
	}

	// Publish to next stage
	err = s.publishStoredEmail(ctx, &dto.EmailStored{
		ID:        email.ID,
		EMLKey:    email.EMLKey,
		MailboxID: event.MailboxID,
	})
	if err != nil {
		eventRecord.ErrorMessage = err.Error()
		spans.TraceError(err)
		return
	}

	return
}

func (s *EmailStorageService) SaveIMAPMessageAsEML(ctx context.Context, msg *imap.Message, emailID string) (string, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailStorageService.SaveIMAPMessageAsEML")
	defer spans.Finish()

	tenant := utils.GetTenantFromContext(ctx)
	if tenant == "" {
		spans.TraceError(mailstack_errors.ErrTenantMissing)
		return "", mailstack_errors.ErrTenantMissing
	}

	// Extract date from message
	var msgTime time.Time
	if msg.Envelope != nil && !msg.Envelope.Date.IsZero() {
		msgTime = msg.Envelope.Date
	} else {
		// Fallback to current time if no date in message
		msgTime = time.Now()
	}

	// Format parts of the path
	year := msgTime.Format("2006")
	month := msgTime.Format("01")

	outputPath := fmt.Sprintf("%s/%s/%s/%s.eml", tenant, year, month, emailID)

	// Get the body reader for the RFC822 format
	r := msg.GetBody(&imap.BodySectionName{})
	if r == nil {
		return "", fmt.Errorf("message body not found")
	}

	// Read the message into a byte array
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		spans.TraceError(err)
		return "", fmt.Errorf("failed to read message body: %w", err)
	}
	emlFile := buf.Bytes()

	// Set the proper MIME content type for EML files
	contentType := "message/rfc822"

	// Upload the file to storage
	err := s.emlStorage.Upload(ctx, outputPath, emlFile, contentType)
	if err != nil {
		err = fmt.Errorf("failed to upload EML file: %w", err)
		spans.TraceError(err)
		return "", err
	}

	return outputPath, nil
}
