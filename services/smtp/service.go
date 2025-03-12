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
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"

	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/tracing"
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
	err := s.validateEmail(ctx, email)
	if err != nil {
		tracing.TraceErr(span, err)
		return &SendResult{
			Success:      false,
			ErrorMessage: err.Error(),
		}
	}

	// Prepare the email message
	allRecipients, messageBuffer, err := s.prepareMessage(ctx, email, attachments)
	if err != nil {
		tracing.TraceErr(span, err)
		return &SendResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("failed to prepare email: %v", err),
		}
	}

	// Send the email
	err = s.sendToServer(ctx, email.FromAddress, allRecipients, messageBuffer)
	if err != nil {
		tracing.TraceErr(span, err)
		email.LastAttemptAt = utils.NowPtr()
		email.Status = enum.EmailStatusFailed
		email.StatusDetail = err.Error()
		err = s.repositories.EmailRepository.Update(ctx, email)
		if err != nil {
			tracing.TraceErr(span, err)
		}
		return &SendResult{
			Success:      false,
			ErrorMessage: err.Error(),
		}
	}

	// update db with success
	email.SentAt = utils.NowPtr()
	email.LastAttemptAt = email.SentAt
	email.Status = enum.EmailStatusSent
	err = s.repositories.EmailRepository.Update(ctx, email)
	if err != nil {
		tracing.TraceErr(span, err)
		return &SendResult{
			Success:      false,
			ErrorMessage: err.Error(),
		}
	}

	return &SendResult{
		Success:   true,
		MessageID: email.MessageID,
	}
}

// validateEmail performs basic validation on the email
func (s *SMTPClient) validateEmail(ctx context.Context, email *models.Email) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "SMTPClient.validateEmail")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	email.Direction = enum.EmailOutbound

	if email == nil {
		err := fmt.Errorf("email cannot be nil")
		tracing.TraceErr(span, err)
		return err
	}

	if email.FromAddress == "" {
		err := fmt.Errorf("from address is required")
		tracing.TraceErr(span, err)
		return err
	}

	if email.FromDomain == "" {
		validation := mailvalidate.ValidateEmailSyntax(email.FromAddress)
		if !validation.IsValid {
			err := fmt.Errorf("from address is not valid")
			tracing.TraceErr(span, err)
			return err
		}
		if validation.Domain != s.mailbox.MailboxDomain {
			err := errors.New("from domain does not match mailbox domain")
			tracing.TraceErr(span, err)
			return err
		}
		email.FromDomain = validation.Domain
	}

	if len(email.ToAddresses) == 0 {
		err := fmt.Errorf("at least one recipient is required")
		tracing.TraceErr(span, err)
		return err
	}

	if email.BodyText == "" && email.BodyHTML == "" {
		err := fmt.Errorf("email must have either text or HTML content")
		tracing.TraceErr(span, err)
		return err
	}

	if email.Subject == "" {
		err := fmt.Errorf("email must have a subject")
		tracing.TraceErr(span, err)
		return err
	}

	if email.MessageID == "" {
		email.MessageID = utils.GenerateMessageID(s.mailbox.MailboxDomain, "")
	}

	return nil
}

// prepareMessage builds the email message in proper MIME format
func (s *SMTPClient) prepareMessage(ctx context.Context, email *models.Email, attattachments []*models.EmailAttachment) ([]string, *bytes.Buffer, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "SMTPClient.prepareMessage")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	// Create message buffer
	buffer := bytes.NewBuffer(nil)

	// Generate headers
	headers := email.BuildHeaders()
	tracing.LogObjectAsJson(span, "headers", headers)

	// Build email content
	var err error
	if email.HasRichContent() {
		err = s.buildMultipartMessage(ctx, email, headers, attattachments, buffer)
	} else {
		err = s.buildPlainTextMessage(email, headers, buffer)
	}

	if err != nil {
		tracing.TraceErr(span, err)
		return nil, nil, err
	}

	return email.AllRecipients(), buffer, nil
}

// buildMultipartMessage creates a multipart MIME message with text, HTML, and attachments
func (s *SMTPClient) buildMultipartMessage(ctx context.Context, email *models.Email, headers map[string]string, attachments []*models.EmailAttachment, buffer *bytes.Buffer) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "SMTPClient.buildMultipartMessage")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	writer := multipart.NewWriter(buffer)
	boundary := writer.Boundary()
	headers["Content-Type"] = "multipart/mixed; boundary=" + boundary

	// Write headers
	writeHeaders(headers, buffer)

	// Add text part if available
	if email.BodyText != "" {
		if err := s.addTextPart(ctx, writer, email.BodyText); err != nil {
			tracing.TraceErr(span, err)
			return err
		}
	}

	// Add HTML part if available
	if email.BodyHTML != "" {
		if err := s.addHtmlPart(ctx, writer, email.BodyHTML); err != nil {
			tracing.TraceErr(span, err)
			return err
		}
	}

	// Add attachments if any
	if attachments == nil || len(attachments) == 0 {
		return writer.Close()
	}

	for _, attachment := range attachments {
		if err := s.addAttachment(ctx, writer, attachment); err != nil {
			tracing.TraceErr(span, err)
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
func (s *SMTPClient) addTextPart(ctx context.Context, writer *multipart.Writer, content string) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "SMTPClient.addTextPart")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	textPart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {"text/plain; charset=UTF-8"},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		err = fmt.Errorf("failed to create text part: %w", err)
		tracing.TraceErr(span, err)
		return err
	}

	_, err = textPart.Write([]byte(content))
	if err != nil {
		err = fmt.Errorf("failed to write text content: %w", err)
		tracing.TraceErr(span, err)
		return err
	}

	return nil
}

// addHtmlPart adds an HTML part to a multipart message
func (s *SMTPClient) addHtmlPart(ctx context.Context, writer *multipart.Writer, content string) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "SMTPClient.addHtmlPart")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	htmlPart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {"text/html; charset=UTF-8"},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		err = fmt.Errorf("failed to create HTML part: %w", err)
		tracing.TraceErr(span, err)
		return err
	}

	_, err = htmlPart.Write([]byte(content))
	if err != nil {
		err = fmt.Errorf("failed to write HTML content: %w", err)
		tracing.TraceErr(span, err)
		return err
	}

	return nil
}

// addAttachment adds an attachment to a multipart message
func (s *SMTPClient) addAttachment(ctx context.Context, writer *multipart.Writer, attachment *models.EmailAttachment) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "SMTPClient.addAttachment")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	if writer == nil {
		err := errors.New("attachment writer cannot be nil")
		tracing.TraceErr(span, err)
		return err
	}
	if attachment == nil {
		err := errors.New("attachment is nil")
		tracing.TraceErr(span, err)
		return err
	}

	attachmentPart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {fmt.Sprintf("%s; name=%q", attachment.ContentType, attachment.Filename)},
		"Content-Disposition":       {fmt.Sprintf("attachment; filename=%q", attachment.Filename)},
		"Content-Transfer-Encoding": {"base64"},
	})
	if err != nil {
		err = fmt.Errorf("failed to create attachment part: %w", err)
		tracing.TraceErr(span, err)
		return err
	}

	// download content from storage
	content, err := s.repositories.EmailAttachmentRepository.GetAttachment(ctx, attachment.ID)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	_, err = attachmentPart.Write(content)
	if err != nil {
		err = fmt.Errorf("failed to write attachment content: %w", err)
		tracing.TraceErr(span, err)
		return err
	}

	return nil
}

// sendToServer sends the prepared email to the SMTP server
func (s *SMTPClient) sendToServer(ctx context.Context, from string, recipients []string, buffer *bytes.Buffer) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "SMTPClient.sendToServer")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	addr := fmt.Sprintf("%s:%d", s.mailbox.SmtpServer, s.mailbox.SmtpPort)
	auth := smtp.PlainAuth("", s.mailbox.SmtpUsername, s.mailbox.SmtpPassword, s.mailbox.SmtpServer)

	if s.mailbox.SmtpSecurity == enum.EmailSecurityStartTLS {
		return s.sendWithSTARTTLS(ctx, addr, auth, from, recipients, buffer)
	}

	// Standard SMTP (may use STARTTLS if server supports it)
	err := smtp.SendMail(addr, auth, from, recipients, buffer.Bytes())
	if err != nil {
		err = fmt.Errorf("failed to send email: %w", err)
		tracing.TraceErr(span, err)
		return err
	}

	return nil
}

func (s *SMTPClient) sendWithSTARTTLS(ctx context.Context, addr string, auth smtp.Auth, from string, recipients []string, buffer *bytes.Buffer) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "SMTPClient.sendWithSTARTTLS")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)
	span.LogKV("smtp_server", s.mailbox.SmtpServer)
	span.LogKV("smtp_port", s.mailbox.SmtpPort)
	span.LogKV("smtp_username", s.mailbox.SmtpUsername)
	span.LogKV("from_address", from)

	// Connect to the server without TLS first
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		err = fmt.Errorf("failed to connect to SMTP server: %w", err)
		tracing.TraceErr(span, err)
		return err
	}
	defer conn.Close()

	// Create SMTP client
	client, err := smtp.NewClient(conn, s.mailbox.SmtpServer)
	if err != nil {
		err = fmt.Errorf("failed to create SMTP client: %w", err)
		tracing.TraceErr(span, err)
		return err
	}
	defer client.Close()

	// Start TLS
	tlsConfig := &tls.Config{
		ServerName: s.mailbox.SmtpServer,
	}
	if err = client.StartTLS(tlsConfig); err != nil {
		err = fmt.Errorf("failed to start TLS: %w", err)
		tracing.TraceErr(span, err)
		return err
	}

	// Authenticate after TLS is established
	if err = client.Auth(auth); err != nil {
		err = fmt.Errorf("SMTP authentication failed: %w", err)
		tracing.TraceErr(span, err)
		return err
	}

	// Set sender
	if err = client.Mail(from); err != nil {
		err = fmt.Errorf("SMTP MAIL command failed: %w", err)
		tracing.TraceErr(span, err)
		return err
	}

	// Set recipients
	for _, recipient := range recipients {
		if err = client.Rcpt(recipient); err != nil {
			err = fmt.Errorf("SMTP RCPT command failed for %s: %w", recipient, err)
			tracing.TraceErr(span, err)
			return err
		}
	}

	// Send data
	dataWriter, err := client.Data()
	if err != nil {
		err = fmt.Errorf("SMTP DATA command failed: %w", err)
		tracing.TraceErr(span, err)
		return err
	}

	_, err = dataWriter.Write(buffer.Bytes())
	if err != nil {
		err = fmt.Errorf("failed to write email data: %w", err)
		tracing.TraceErr(span, err)
		return err
	}

	err = dataWriter.Close()
	if err != nil {
		err = fmt.Errorf("failed to close data writer: %w", err)
		tracing.TraceErr(span, err)
		return err
	}

	return client.Quit()
}

// sendWithExplicitTLS sends an email using explicit TLS connection
func (s *SMTPClient) sendWithExplicitTLS(ctx context.Context, addr string, auth smtp.Auth, from string, recipients []string, buffer *bytes.Buffer) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "SMTPClient.sendWithExplicitTLS")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)
	span.LogKV("address", addr)

	// Create TLS config
	tlsConfig := &tls.Config{
		ServerName: s.mailbox.SmtpServer,
	}

	// Connect to the server
	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		err = fmt.Errorf("failed to connect to SMTP server: %w", err)
		tracing.TraceErr(span, err)
		return err
	}
	defer conn.Close()

	// Create SMTP client
	client, err := smtp.NewClient(conn, s.mailbox.SmtpServer)
	if err != nil {
		err = fmt.Errorf("failed to create SMTP client: %w", err)
		tracing.TraceErr(span, err)
		return err
	}
	defer client.Close()

	// Authenticate
	if err = client.Auth(auth); err != nil {
		err = fmt.Errorf("SMTP authentication failed: %w", err)
		tracing.TraceErr(span, err)
		return err
	}

	// Set sender
	if err = client.Mail(from); err != nil {
		err = fmt.Errorf("SMTP MAIL command failed: %w", err)
		tracing.TraceErr(span, err)
		return err
	}

	// Set recipients
	for _, recipient := range recipients {
		if err = client.Rcpt(recipient); err != nil {
			err = fmt.Errorf("SMTP RCPT command failed for %s: %w", recipient, err)
			tracing.TraceErr(span, err)
			return err
		}
	}

	// Send data
	dataWriter, err := client.Data()
	if err != nil {
		err = fmt.Errorf("SMTP DATA command failed: %w", err)
		tracing.TraceErr(span, err)
		return err
	}

	_, err = dataWriter.Write(buffer.Bytes())
	if err != nil {
		err = fmt.Errorf("failed to write email data: %w", err)
		tracing.TraceErr(span, err)
		return err
	}

	err = dataWriter.Close()
	if err != nil {
		err = fmt.Errorf("failed to close data writer: %w", err)
		tracing.TraceErr(span, err)
		return err
	}

	return client.Quit()
}
