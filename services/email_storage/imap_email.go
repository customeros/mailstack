package email_storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/emersion/go-imap"

	mailstack_errors "github.com/customeros/mailstack/internal/errors"
	"github.com/customeros/mailstack/internal/telemetry"
	"github.com/customeros/mailstack/internal/utils"
	"github.com/customeros/mailstack/proto/pb"
)

// HandleRawEmail processes a single raw email message
func (s *emailStorageService) handleIMAPEmail(ctx context.Context, event *pb.EmailReceivedIMAP) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "EmailStorageService.handleIMAPEmail")
	defer spans.Finish()
	spans.LogObjectAsJson("receivedIMAPEmailEvent", event)

	emailLog := s.NewEmailLog()

	emailLog.EmailHash = utils.GenerateIMAPHash(event.MailboxId, event.Folder, event.ImapUid)

	emailExists, err := s.repositories.EmailLogRepository.IsDuplicateByHash(ctx, emailLog.EmailHash)
	if err != nil {
		spans.TraceError(err)
		return err
	}
	if emailExists {
		spans.LogKV("duplicateEmail.hash", emailLog.EmailHash)
		// TODO either publish a skip notification or FIX import so no dups
		return nil
	}

	// get message from imap server
	msg, err := s.imapService.GetMessageByUID(ctx, event.MailboxId, event.Folder, event.ImapUid)
	if err != nil {
		spans.TraceError(err)
		return err
	}

	// save message as .eml
	bucketKey, err := s.saveIMAPMessageAsEMLToBucket(ctx, msg, emailLog.ID, event.MailboxId)
	if err != nil {
		spans.TraceError(err)
		return err
	}

	// build email record
	emailLog.MailboxID = event.MailboxId
	emailLog.EMLKey = bucketKey
	emailLog.MessageID = utils.NormalizeMessageID(msg.Envelope.MessageId)
	emailLog.Subject = msg.Envelope.Subject
	emailLog.CleanSubject = utils.NormalizeSubject(msg.Envelope.Subject)

	// log email
	err = s.repositories.EmailLogRepository.Create(ctx, emailLog)
	if err != nil {
		spans.TraceError(err)
		return err
	}

	// Publish to next stage
	err = s.publishStoredEmail(ctx, &pb.EmailStored{
		EmailId:   emailLog.ID,
		EmlKey:    emailLog.EMLKey,
		MailboxId: event.MailboxId,
	})
	if err != nil {
		spans.TraceError(err)
		return err
	}

	return nil
}

func (s *emailStorageService) saveIMAPMessageAsEMLToBucket(ctx context.Context, msg *imap.Message, emailID, mailboxId string) (string, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "EmailStorageService.saveIMAPMessageAsEMLToBucket")
	defer spans.Finish()
	spans.TagEntity(emailID)
	spans.LogKV("mailboxId", mailboxId)

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

	outputPath := fmt.Sprintf("%s/%s/%s/%s/%s.eml", tenant, mailboxId, year, month, emailID)

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

	spans.LogKV("result.outputPath", outputPath)

	return outputPath, nil
}
