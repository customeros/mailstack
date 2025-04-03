package smtp

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"mime/multipart"
	"net"
	"net/smtp"
	"net/textproto"

	"github.com/customeros/mailsherpa/mailvalidate"
	"github.com/pkg/errors"

	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/telemetry"
	"github.com/customeros/mailstack/internal/utils"
)

type SMTPClient struct {
	repositories *repository.Repositories
	mailbox      *models.Mailbox
}

func NewSMTPClient(repos *repository.Repositories, mailbox *models.Mailbox) *SMTPClient {
	return &SMTPClient{
		repositories: repos,
		mailbox:      mailbox,
	}
}

func (s *SMTPClient) Send(ctx context.Context, email *models.EmailLog, attachments []*models.EmailAttachment) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "SMTPClient.Send")
	defer spans.Finish()
	if email == nil {
		spans.TraceError(fmt.Errorf("email cannot be nil"))
		return fmt.Errorf("email cannot be nil")
	}
	spans.TagEntity(email.ID)

	// Validate the email
	err := s.validateEmail(ctx, email)
	if err != nil {
		spans.TraceError(err)
		return err
	}

	// Prepare the email message
	allRecipients, messageBuffer, err := s.prepareMessage(ctx, email, attachments)
	if err != nil {
		spans.TraceError(err)
		return err
	}

	// Create email in db
	// TODO FIX THIS
	email.ID, err = s.repositories.EmailRepository.Create(ctx, &models.Email{})
	if err != nil {
		spans.TraceError(err)
		return err
	}

	// Send the email
	err = s.sendToServer(ctx, email.FromAddress, allRecipients, messageBuffer)
	if err != nil {
		spans.TraceError(err)
		email.LastAttemptAt = utils.NowPtr()
		email.Status = enum.EmailStatusFailed
		email.StatusDetail = err.Error()
		// TODO FIX
		err = s.repositories.EmailRepository.Update(ctx, &models.Email{})
		if err != nil {
			spans.TraceError(err)
		}

		// TODO write to timescale
	}

	// update db with success
	email.SentAt = utils.NowPtr()
	email.LastAttemptAt = email.SentAt
	email.Status = enum.EmailStatusSent
	// TODO FIX
	err = s.repositories.EmailRepository.Update(ctx, &models.Email{})
	if err != nil {
		spans.TraceError(err)
	}
	// TODO write to timescale

	return err
}

// validateEmail performs basic validation on the email
func (s *SMTPClient) validateEmail(ctx context.Context, email *models.EmailLog) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "SMTPClient.validateEmail")
	defer spans.Finish()

	if email == nil {
		err := fmt.Errorf("email cannot be nil")
		spans.TraceError(err)
		return err
	}
	spans.TagEntity(email.ID)
	email.Direction = enum.EmailDirectionOutbound

	if email.FromAddress == "" {
		err := fmt.Errorf("from address is required")
		spans.TraceError(err)
		return err
	}

	if email.FromDomain == "" {
		validation := mailvalidate.ValidateEmailSyntax(email.FromAddress)
		if !validation.IsValid {
			err := fmt.Errorf("from address is not valid")
			spans.TraceError(err)
			return err
		}
		if validation.Domain != s.mailbox.MailboxDomain {
			err := errors.New("from domain does not match mailbox domain")
			spans.TraceError(err)
			return err
		}
		email.FromDomain = validation.Domain
		email.FromUser = validation.User
	}

	if len(email.ToAddresses) == 0 {
		err := fmt.Errorf("at least one recipient is required")
		spans.TraceError(err)
		return err
	}

	if email.BodyText == "" && email.BodyHtml == "" {
		err := fmt.Errorf("email must have either text or HTML content")
		spans.TraceError(err)
		return err
	}

	if email.Subject == "" {
		err := fmt.Errorf("email must have a subject")
		spans.TraceError(err)
		return err
	}

	if email.MessageID == "" {
		email.MessageID = utils.GenerateMessageID(s.mailbox.MailboxDomain, "")
	}

	return nil
}

// prepareMessage builds the email message in proper MIME format and stores raw metadata
func (s *SMTPClient) prepareMessage(ctx context.Context, email *models.EmailLog, attachments []*models.EmailAttachment) ([]string, *bytes.Buffer, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "SMTPClient.prepareMessage")
	defer spans.Finish()
	if email == nil {
		spans.TraceError(fmt.Errorf("email cannot be nil"))
		return nil, nil, fmt.Errorf("email cannot be nil")
	}
	spans.TagEntity(email.ID)

	// Create message buffer
	buffer := bytes.NewBuffer(nil)

	// Generate and store headers
	headers := s.prepareHeaders(ctx, email)
	spans.LogObjectAsJson("headers", headers)

	// Prepare message content and body structure
	var err error
	// TODO
	// if email.HasRichContent() {
	// 	err = s.buildMultipartMessageWithStructure(ctx, email, headers, attachments, buffer)
	// } else {
	err = s.buildPlainTextMessageWithStructure(ctx, email, headers, buffer)
	// }
	if err != nil {
		spans.TraceError(err)
		return nil, nil, err
	}

	// TODO FIX THIS
	return nil, buffer, nil
}

// prepareHeaders generates email headers and stores them in the Email model
func (s *SMTPClient) prepareHeaders(ctx context.Context, email *models.EmailLog) map[string]string {
	// TODO FIX
	return nil
}

// buildMultipartMessageWithStructure creates a multipart MIME message with text, HTML, and attachments
// while also capturing body structure metadata
func (s *SMTPClient) buildMultipartMessageWithStructure(ctx context.Context, email *models.EmailLog,
	headers map[string]string, attachments []*models.EmailAttachment, buffer *bytes.Buffer,
) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "SMTPClient.buildMultipartMessageWithStructure")
	defer spans.Finish()
	if email == nil {
		spans.TraceError(fmt.Errorf("email cannot be nil"))
		return fmt.Errorf("email cannot be nil")
	}
	spans.TagEntity(email.ID)

	writer := multipart.NewWriter(buffer)
	boundary := writer.Boundary()

	// Set content-type in headers
	headers["Content-Type"] = "multipart/mixed; boundary=" + boundary

	// Initialize body structure
	bodyStructure := initializeBodyStructure(email)
	bodyStructure["type"] = "multipart/mixed"
	bodyStructure["boundary"] = boundary

	// Initialize parts array for body structure
	parts := []models.JSONMap{}
	hasTextPart := false
	hasHtmlPart := false

	// Write headers to buffer
	writeHeaders(headers, buffer)

	// Add text part if available
	if email.BodyText != "" {
		if err := addTextPart(ctx, writer, email.BodyText); err != nil {
			return err
		}
		parts = append(parts, createPartMetadata("text/plain", len(email.BodyText), ""))
		hasTextPart = true
	}

	// Add HTML part if available
	if email.BodyHtml != "" {
		if err := addHtmlPart(ctx, writer, email.BodyHtml); err != nil {
			return err
		}
		parts = append(parts, createPartMetadata("text/html", len(email.BodyHtml), ""))
		hasHtmlPart = true
	}

	// Add attachments if any
	if attachments != nil && len(attachments) > 0 {
		for _, attachment := range attachments {
			if err := s.addAttachment(ctx, writer, attachment); err != nil {
				return err
			}
			parts = append(parts, createAttachmentMetadata(attachment))
		}
	}

	// Update body structure with parts information
	bodyStructure["parts"] = parts
	bodyStructure["hasTextPart"] = hasTextPart
	bodyStructure["hasHtmlPart"] = hasHtmlPart

	// Close the multipart writer
	return writer.Close()
}

// buildPlainTextMessageWithStructure creates a simple text-only email and captures body structure
func (s *SMTPClient) buildPlainTextMessageWithStructure(ctx context.Context, email *models.EmailLog,
	headers map[string]string, buffer *bytes.Buffer,
) error {
	headers["Content-Type"] = "text/plain; charset=UTF-8"

	// Initialize body structure for plain text
	bodyStructure := initializeBodyStructure(email)
	bodyStructure["type"] = "text/plain"
	bodyStructure["charset"] = "UTF-8"
	bodyStructure["hasTextPart"] = true
	bodyStructure["hasHtmlPart"] = false
	bodyStructure["size"] = len(email.BodyText)

	// Write headers to buffer
	writeHeaders(headers, buffer)

	// Write body
	_, err := buffer.WriteString(email.BodyText)
	return err
}

// initializeBodyStructure creates the base body structure object
func initializeBodyStructure(email *models.EmailLog) models.JSONMap {
	return models.JSONMap{
		"hasAttachments": email.HasAttachment,
	}
}

// createPartMetadata creates metadata for a message part
func createPartMetadata(contentType string, size int, id string) models.JSONMap {
	part := models.JSONMap{
		"type":     contentType,
		"charset":  "UTF-8",
		"encoding": "quoted-printable",
		"size":     size,
	}

	if id != "" {
		part["id"] = id
	}

	return part
}

// createAttachmentMetadata creates metadata for an attachment part
func createAttachmentMetadata(attachment *models.EmailAttachment) models.JSONMap {
	return models.JSONMap{
		"type":        attachment.ContentType,
		"name":        attachment.Filename,
		"disposition": "attachment",
		"encoding":    "base64",
		"size":        attachment.Size,
		"id":          attachment.ID,
	}
}

// writeHeaders writes email headers to the buffer
func writeHeaders(headers map[string]string, buffer *bytes.Buffer) {
	for k, v := range headers {
		buffer.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	buffer.WriteString("\r\n")
}

// addTextPart adds a plain text part to a multipart message
func addTextPart(ctx context.Context, writer *multipart.Writer, content string) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "SMTPClient.addTextPart")
	defer spans.Finish()

	textPart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {"text/plain; charset=UTF-8"},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		err = fmt.Errorf("failed to create text part: %w", err)
		spans.TraceError(err)
		return err
	}

	_, err = textPart.Write([]byte(content))
	if err != nil {
		err = fmt.Errorf("failed to write text content: %w", err)
		spans.TraceError(err)
		return err
	}

	return nil
}

// addHtmlPart adds an HTML part to a multipart message
func addHtmlPart(ctx context.Context, writer *multipart.Writer, content string) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "SMTPClient.addHtmlPart")
	defer spans.Finish()

	htmlPart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {"text/html; charset=UTF-8"},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		err = fmt.Errorf("failed to create HTML part: %w", err)
		spans.TraceError(err)
		return err
	}

	_, err = htmlPart.Write([]byte(content))
	if err != nil {
		err = fmt.Errorf("failed to write HTML content: %w", err)
		spans.TraceError(err)
		return err
	}

	return nil
}

// addAttachment adds an attachment to a multipart message
func (s *SMTPClient) addAttachment(ctx context.Context, writer *multipart.Writer, attachment *models.EmailAttachment) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "SMTPClient.addAttachment")
	defer spans.Finish()

	if writer == nil {
		err := errors.New("attachment writer cannot be nil")
		spans.TraceError(err)
		return err
	}
	if attachment == nil {
		err := errors.New("attachment is nil")
		spans.TraceError(err)
		return err
	}

	attachmentPart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {fmt.Sprintf("%s; name=%q", attachment.ContentType, attachment.Filename)},
		"Content-Disposition":       {fmt.Sprintf("attachment; filename=%q", attachment.Filename)},
		"Content-Transfer-Encoding": {"base64"},
	})
	if err != nil {
		err = fmt.Errorf("failed to create attachment part: %w", err)
		spans.TraceError(err)
		return err
	}

	// download content from storage
	content, err := s.repositories.EmailAttachmentRepository.DownloadAttachment(ctx, attachment.ID)
	if err != nil {
		spans.TraceError(err)
		return err
	}

	_, err = attachmentPart.Write(content)
	if err != nil {
		err = fmt.Errorf("failed to write attachment content: %w", err)
		spans.TraceError(err)
		return err
	}

	return nil
}

// sendToServer sends the prepared email to the SMTP server
func (s *SMTPClient) sendToServer(ctx context.Context, from string, recipients []string, buffer *bytes.Buffer) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "SMTPClient.sendToServer")
	defer spans.Finish()

	addr := fmt.Sprintf("%s:%d", s.mailbox.SmtpServer, s.mailbox.SmtpPort)
	auth := smtp.PlainAuth("", s.mailbox.SmtpUsername, s.mailbox.SmtpPassword, s.mailbox.SmtpServer)

	if s.mailbox.SmtpSecurity == enum.EmailSecurityStartTLS {
		return s.sendWithSTARTTLS(ctx, addr, auth, from, recipients, buffer)
	}

	// Standard SMTP (may use STARTTLS if server supports it)
	err := smtp.SendMail(addr, auth, from, recipients, buffer.Bytes())
	if err != nil {
		err = fmt.Errorf("failed to send email: %w", err)
		spans.TraceError(err)
		return err
	}

	return nil
}

func (s *SMTPClient) sendWithSTARTTLS(ctx context.Context, addr string, auth smtp.Auth, from string, recipients []string, buffer *bytes.Buffer) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "SMTPClient.sendWithSTARTTLS")
	defer spans.Finish()
	spans.TagEntity(s.mailbox.ID)
	spans.LogKV("smtp_server", s.mailbox.SmtpServer)
	spans.LogKV("smtp_port", s.mailbox.SmtpPort)
	spans.LogKV("smtp_username", s.mailbox.SmtpUsername)
	spans.LogKV("from_address", from)

	// Connect to the server without TLS first
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		err = fmt.Errorf("failed to connect to SMTP server: %w", err)
		spans.TraceError(err)
		return err
	}
	defer conn.Close()

	// Create SMTP client
	client, err := smtp.NewClient(conn, s.mailbox.SmtpServer)
	if err != nil {
		err = fmt.Errorf("failed to create SMTP client: %w", err)
		spans.TraceError(err)
		return err
	}
	defer client.Close()

	// Start TLS
	tlsConfig := &tls.Config{
		ServerName: s.mailbox.SmtpServer,
	}
	if err = client.StartTLS(tlsConfig); err != nil {
		err = fmt.Errorf("failed to start TLS: %w", err)
		spans.TraceError(err)
		return err
	}

	// Authenticate after TLS is established
	if err = client.Auth(auth); err != nil {
		err = fmt.Errorf("SMTP authentication failed: %w", err)
		spans.TraceError(err)
		return err
	}

	// Set sender
	if err = client.Mail(from); err != nil {
		err = fmt.Errorf("SMTP MAIL command failed: %w", err)
		spans.TraceError(err)
		return err
	}

	// Set recipients
	for _, recipient := range recipients {
		if err = client.Rcpt(recipient); err != nil {
			err = fmt.Errorf("SMTP RCPT command failed for %s: %w", recipient, err)
			spans.TraceError(err)
			return err
		}
	}

	// Send data
	dataWriter, err := client.Data()
	if err != nil {
		err = fmt.Errorf("SMTP DATA command failed: %w", err)
		spans.TraceError(err)
		return err
	}

	_, err = dataWriter.Write(buffer.Bytes())
	if err != nil {
		err = fmt.Errorf("failed to write email data: %w", err)
		spans.TraceError(err)
		return err
	}

	err = dataWriter.Close()
	if err != nil {
		err = fmt.Errorf("failed to close data writer: %w", err)
		spans.TraceError(err)
		return err
	}

	return client.Quit()
}

// sendWithExplicitTLS sends an email using explicit TLS connection
func (s *SMTPClient) sendWithExplicitTLS(ctx context.Context, addr string, auth smtp.Auth, from string, recipients []string, buffer *bytes.Buffer) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "SMTPClient.sendWithExplicitTLS")
	defer spans.Finish()
	spans.TagEntity(s.mailbox.ID)
	spans.LogKV("address", addr)

	// Create TLS config
	tlsConfig := &tls.Config{
		ServerName: s.mailbox.SmtpServer,
	}

	// Connect to the server
	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		err = fmt.Errorf("failed to connect to SMTP server: %w", err)
		spans.TraceError(err)
		return err
	}
	defer conn.Close()

	// Create SMTP client
	client, err := smtp.NewClient(conn, s.mailbox.SmtpServer)
	if err != nil {
		err = fmt.Errorf("failed to create SMTP client: %w", err)
		spans.TraceError(err)
		return err
	}
	defer client.Close()

	// Authenticate
	if err = client.Auth(auth); err != nil {
		err = fmt.Errorf("SMTP authentication failed: %w", err)
		spans.TraceError(err)
		return err
	}

	// Set sender
	if err = client.Mail(from); err != nil {
		err = fmt.Errorf("SMTP MAIL command failed: %w", err)
		spans.TraceError(err)
		return err
	}

	// Set recipients
	for _, recipient := range recipients {
		if err = client.Rcpt(recipient); err != nil {
			err = fmt.Errorf("SMTP RCPT command failed for %s: %w", recipient, err)
			spans.TraceError(err)
			return err
		}
	}

	// Send data
	dataWriter, err := client.Data()
	if err != nil {
		err = fmt.Errorf("SMTP DATA command failed: %w", err)
		spans.TraceError(err)
		return err
	}

	_, err = dataWriter.Write(buffer.Bytes())
	if err != nil {
		err = fmt.Errorf("failed to write email data: %w", err)
		spans.TraceError(err)
		return err
	}

	err = dataWriter.Close()
	if err != nil {
		err = fmt.Errorf("failed to close data writer: %w", err)
		spans.TraceError(err)
		return err
	}

	return client.Quit()
}
