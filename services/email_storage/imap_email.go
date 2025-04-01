package email_storage

import (
	"context"

	"github.com/customeros/mailstack/dto"
	"github.com/customeros/mailstack/internal/telemetry"
)

// HandleRawEmail processes a single raw email message
func (s *emailStorageService) HandleIMAPEmail(ctx context.Context, emailEvent dto.EmailReceivedIMAP) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailStorageService.handleRawEmail")
	defer spans.Finish()
	spans.LogObjectAsJson("emailEvent", emailEvent)

	emailExists, err := s.imapEmailExists(ctx, emailEvent)
	if err != nil {
		spans.TraceError(err)
		return err
	}
	if emailExists {
		// Log duplicate detection
		spans.LogKV("status", "skipped_duplicate")
		// Log the skipped event in TimescaleDB with "skipped" status
		err = s.logEmailEvent(ctx, "emails.inbound.skipped", emailEvent.Email, map[string]interface{}{
			"reason": "duplicate",
			"hash":   emailEvent.Email.EmailHash,
		})
		if err != nil {
			spans.TraceError(err)
		}
	}

	// get message from imap server
	msg, err := s.imapService.GetMessageByUID(ctx, inboundEmail.MailboxID, inboundEmail.Folder, inboundEmail.ImapUID)
	if err != nil {
		spans.TraceError(err)
		return err
	}

	// save message as .eml
	bucketKey, err := s.SaveIMAPMessageAsEML(ctx, msg, email.ID)
	if err != nil {
		spans.TraceError(err)
		return err
	}

	// Log the event in TimescaleDB
	err = s.logEmailEvent(ctx, "emails.inbound.stored", email)
	if err != nil {
		spans.TraceError(err)
		// Don't return error here, continue with publishing
		log.Printf("Warning: Failed to log email event: %v", err)
	}

	// Publish to next stage
	err = s.PublishStoredEmail(ctx, email)
	if err != nil {
		spans.TraceError(err)
		return fmt.Errorf("failed to publish stored email: %w", err)
	}

	return nil
}

func (s *emailStorageService) imapEmailExists(ctx context.Context, emailEvent dto.EmailReceivedIMAP) (bool, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailStorageService.handleRawEmail")
	defer spans.Finish()

	emailHash := utils.GenerateIMAPHash(emailEvent.MailboxID, emailEvent.Folder, emailEvent.ImapUID)

	return s.repositories.EmailEventRepository.IsDuplicateByHash(ctx, emailHash)
}

func (s *emailStorageService) SaveIMAPMessageAsEML(ctx context.Context, msg *go_imap.Message, emailID string) (string, error) {
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
	r := msg.GetBody(&go_imap.BodySectionName{})
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
	err := s.storageService.Upload(ctx, outputPath, emlFile, contentType)
	if err != nil {
		err = fmt.Errorf("failed to upload EML file: %w", err)
		spans.TraceError(err)
		return "", err
	}

	return outputPath, nil
}
