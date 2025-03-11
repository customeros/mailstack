package smtp

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"mime/multipart"
	"net/smtp"
	"net/textproto"
	"strings"

	"github.com/customeros/customeros/packages/server/customer-os-common-module/tracing"
	"github.com/customeros/mailsherpa/mailvalidate"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"

	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/utils"
)

type SMTPClient struct {
	mailbox *models.Mailbox
}

func NewSMTPClient(mailbox *models.Mailbox) *SMTPClient {
	return &SMTPClient{
		mailbox: mailbox,
	}
}

type SendResult struct {
	Success      bool
	MessageID    string
	ErrorMessage string
}

func (s *SMTPClient) Send(ctx context.Context, email *models.Email, attachments []*models.EmailAttachment) *SendResult {
	span, ctx := opentracing.StartSpanFromContext(ctx, "SMTPClient.Send")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	// Validate the email
	if err := s.validateEmail(email); err != nil {
		return &SendResult{
			Success:      false,
			ErrorMessage: err.Error(),
		}
	}

	// Prepare the email message
	allRecipients, messageBuffer, err := s.prepareMessage(email, attachments)
	if err != nil {
		return &SendResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("failed to prepare email: %v", err),
		}
	}

	// Send the email
	err = s.sendToServer(email.FromAddress, allRecipients, messageBuffer)
	if err != nil {
		return &SendResult{
			Success:      false,
			ErrorMessage: err.Error(),
		}
	}

	return &SendResult{
		Success:   true,
		MessageID: utils.GenerateMessageID(s.mailbox.MailboxDomain, ""),
	}
}

// validateEmail performs basic validation on the email
func (s *SMTPClient) validateEmail(email *models.Email) error {
	if email == nil {
		return fmt.Errorf("email cannot be nil")
	}

	if email.FromAddress == "" {
		return fmt.Errorf("from address is required")
	}

	if email.FromDomain == "" {
		validation := mailvalidate.ValidateEmailSyntax(email.FromAddress)
		if !validation.IsValid {
			return fmt.Errorf("from address is not valid")
		}
		email.FromDomain = validation.Domain
	}

	if len(email.ToAddresses) == 0 {
		return fmt.Errorf("at least one recipient is required")
	}

	if email.BodyText == "" && email.BodyHTML == "" {
		return fmt.Errorf("email must have either text or HTML content")
	}

	if email.Subject == "" {
		return fmt.Errorf("email must have a subject")
	}

	return nil
}

// prepareMessage builds the email message in proper MIME format
func (s *SMTPClient) prepareMessage(email *models.Email, attattachments []*models.EmailAttachment) ([]string, *bytes.Buffer, error) {
	// Collect all recipients
	allRecipients := collectRecipients(email)

	// Create headers
	headers := buildHeaders(email)

	// Create message buffer
	buffer := bytes.NewBuffer(nil)

	// Build email content
	var err error
	if hasRichContent(email) {
		err = s.buildMultipartMessage(email, headers, attattachments, buffer)
	} else {
		err = s.buildPlainTextMessage(email, headers, buffer)
	}

	if err != nil {
		return nil, nil, err
	}

	return allRecipients, buffer, nil
}

// collectRecipients gathers all recipients from To, Cc, and Bcc
func collectRecipients(email *models.Email) []string {
	return append(append(email.ToAddresses, email.CcAddresses...), email.BccAddresses...)
}

// buildHeaders creates a map of email headers
func buildHeaders(email *models.Email) map[string]string {
	header := make(map[string]string)
	header["From"] = email.FromAddress
	header["To"] = strings.Join(email.ToAddresses, ", ")
	if len(email.CcAddresses) > 0 {
		header["Cc"] = strings.Join(email.CcAddresses, ", ")
	}
	header["Subject"] = email.Subject
	header["MIME-Version"] = "1.0"

	// Add any custom headers from the email
	for k, v := range email.Headers {
		if _, exists := header[k]; !exists {
			header[k] = v
		}
	}

	return header
}

// hasRichContent returns true if the email has HTML content or attachments
func hasRichContent(email *models.Email) bool {
	return email.BodyHTML != "" || email.HasAttachment
}

// buildMultipartMessage creates a multipart MIME message with text, HTML, and attachments
func (s *SMTPClient) buildMultipartMessage(email *models.Email, headers map[string]string, attachments []*models.EmailAttachment, buffer *bytes.Buffer) error {
	writer := multipart.NewWriter(buffer)
	boundary := writer.Boundary()
	headers["Content-Type"] = "multipart/mixed; boundary=" + boundary

	// Write headers
	writeHeaders(headers, buffer)

	// Add text part if available
	if email.BodyText != "" {
		if err := addTextPart(writer, email.BodyText); err != nil {
			return err
		}
	}

	// Add HTML part if available
	if email.BodyHTML != "" {
		if err := addHtmlPart(writer, email.BodyHTML); err != nil {
			return err
		}
	}

	// Add attachments if any
	if attachments == nil || len(attachments) == 0 {
		return writer.Close()
	}

	for _, attachment := range attachments {
		if err := addAttachment(writer, attachment); err != nil {
			return err
		}
	}

	return writer.Close()
}

// buildPlainTextMessage creates a simple text-only email
func (s *SMTPClient) buildPlainTextMessage(email *models.Email, headers map[string]string, buffer *bytes.Buffer) error {
	headers["Content-Type"] = "text/plain; charset=UTF-8"

	// Write headers
	writeHeaders(headers, buffer)

	// Write body
	_, err := buffer.WriteString(email.BodyText)
	return err
}

// writeHeaders writes email headers to the buffer
func writeHeaders(headers map[string]string, buffer *bytes.Buffer) {
	for k, v := range headers {
		buffer.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	buffer.WriteString("\r\n")
}

// addTextPart adds a plain text part to a multipart message
func addTextPart(writer *multipart.Writer, content string) error {
	textPart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {"text/plain; charset=UTF-8"},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		return fmt.Errorf("failed to create text part: %w", err)
	}

	_, err = textPart.Write([]byte(content))
	if err != nil {
		return fmt.Errorf("failed to write text content: %w", err)
	}

	return nil
}

// addHtmlPart adds an HTML part to a multipart message
func addHtmlPart(writer *multipart.Writer, content string) error {
	htmlPart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {"text/html; charset=UTF-8"},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		return fmt.Errorf("failed to create HTML part: %w", err)
	}

	_, err = htmlPart.Write([]byte(content))
	if err != nil {
		return fmt.Errorf("failed to write HTML content: %w", err)
	}

	return nil
}

// addAttachment adds an attachment to a multipart message
func addAttachment(writer *multipart.Writer, attachment *models.EmailAttachment) error {
	if writer == nil {
		return errors.New("attachment writer cannot be nil")
	}
	if attachment == nil {
		return errors.New("attachment is nil")
	}

	attachmentPart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {fmt.Sprintf("%s; name=%q", attachment.ContentType, attachment.Filename)},
		"Content-Disposition":       {fmt.Sprintf("attachment; filename=%q", attachment.Filename)},
		"Content-Transfer-Encoding": {"base64"},
	})
	if err != nil {
		return fmt.Errorf("failed to create attachment part: %w", err)
	}

	// download content from storage

	_, err = attachmentPart.Write(content)
	if err != nil {
		return fmt.Errorf("failed to write attachment content: %w", err)
	}

	return nil
}

// sendToServer sends the prepared email to the SMTP server
func (s *SMTPClient) sendToServer(from string, recipients []string, buffer *bytes.Buffer) error {
	addr := fmt.Sprintf("%s:%d", s.mailbox.MailboxDomain, s.mailbox.SmtpPort)
	auth := smtp.PlainAuth("", s.mailbox.SmtpUsername, s.mailbox.SmtpPassword, s.mailbox.SmtpServer)

	// todo make an enum
	if s.mailbox.SmtpSecurity == "tls" {
		return s.sendWithExplicitTLS(addr, auth, from, recipients, buffer)
	}

	// Standard SMTP (may use STARTTLS if server supports it)
	err := smtp.SendMail(addr, auth, from, recipients, buffer.Bytes())
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

// sendWithExplicitTLS sends an email using explicit TLS connection
func (s *SMTPClient) sendWithExplicitTLS(addr string, auth smtp.Auth, from string, recipients []string, buffer *bytes.Buffer) error {
	// Create TLS config
	tlsConfig := &tls.Config{
		ServerName: s.mailbox.SmtpServer,
	}

	// Connect to the server
	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer conn.Close()

	// Create SMTP client
	client, err := smtp.NewClient(conn, s.mailbox.SmtpServer)
	if err != nil {
		return fmt.Errorf("failed to create SMTP client: %w", err)
	}
	defer client.Close()

	// Authenticate
	if err = client.Auth(auth); err != nil {
		return fmt.Errorf("SMTP authentication failed: %w", err)
	}

	// Set sender
	if err = client.Mail(from); err != nil {
		return fmt.Errorf("SMTP MAIL command failed: %w", err)
	}

	// Set recipients
	for _, recipient := range recipients {
		if err = client.Rcpt(recipient); err != nil {
			return fmt.Errorf("SMTP RCPT command failed for %s: %w", recipient, err)
		}
	}

	// Send data
	dataWriter, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA command failed: %w", err)
	}

	_, err = dataWriter.Write(buffer.Bytes())
	if err != nil {
		return fmt.Errorf("failed to write email data: %w", err)
	}

	err = dataWriter.Close()
	if err != nil {
		return fmt.Errorf("failed to close data writer: %w", err)
	}

	return client.Quit()
}
