package emails

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"

	custom_err "github.com/customeros/mailstack/api/errors"
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"
	"github.com/customeros/mailstack/services/smtp"
)

// SendEmailRequest represents the API request for sending an email
type SendEmailRequest struct {
	MailboxID    string        `json:"mailboxId"`
	FromAddress  string        `json:"fromAddress"`
	FromName     string        `json:"fromName"`
	ToAddresses  []string      `json:"toAddresses"`
	CCAddresses  []string      `json:"ccAddresses"`
	BCCAddresses []string      `json:"bccAddresses"`
	ReplyTo      string        `json:"replyTo"`
	Subject      string        `json:"subject"`
	Body         EmailBody     `json:"body"`
	Attachments  []Attachments `json:"attachments"`
	ScheduleFor  time.Time     `json:"scheduleFor"`
}

// EmailBody contains the content of the email
type EmailBody struct {
	Text string `json:"text"`
	HTML string `json:"html"`
}

// Attachments represents a reference to an attachment
type Attachments struct {
	ID string `json:"id"`
}

// EmailContainer holds all the data needed to send an email
type EmailContainer struct {
	Mailbox     *models.Mailbox
	Email       *models.Email
	Attachments []*models.EmailAttachment
}

// Send handles the HTTP request to send a new email
func (h *EmailsHandler) Send() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, span := tracing.StartHttpServerTracerSpanWithHeader(c.Request.Context(), "EmailsHandler.Send", c.Request.Header)
		defer span.Finish()
		tracing.TagComponentRest(span)
		tracing.TagTenant(span, utils.GetTenantFromContext(ctx))

		// Parse the request
		var request SendEmailRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			h.respondWithError(c, span, http.StatusBadRequest, "Invalid request format", err)
			return
		}

		// Validate the request
		errs := h.validateRequest(ctx, &request)
		if errs.HasErrors() {
			tracing.TraceErr(span, errs)
			c.JSON(http.StatusBadRequest, errs)
			return
		}

		// Build email container
		emailContainer, err := h.buildSendEmailContainer(ctx, &request)
		if err != nil {
			h.respondWithError(c, span, http.StatusInternalServerError, "Failed to prepare email", err)
			return
		}

		// Process and send the email
		result, err := h.processAndSendEmail(ctx, emailContainer)
		if err != nil {
			h.respondWithError(c, span, http.StatusInternalServerError, "Failed to send email", err)
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"messageId": result.MessageID,
		})
	}
}

// Helper method to respond with an error
func (h *EmailsHandler) respondWithError(c *gin.Context, span opentracing.Span, statusCode int, message string, err error) {
	tracing.TraceErr(span, err)
	c.JSON(statusCode, gin.H{"error": message, "details": err.Error()})
}

// validateRequest performs initial validation on the request
func (h *EmailsHandler) validateRequest(ctx context.Context, request *SendEmailRequest) *custom_err.MultiErrors {
	span, ctx := opentracing.StartSpanFromContext(ctx, "EmailsHandler.validateRequest")
	defer span.Finish()
	tracing.TagComponentRest(span)

	errs := custom_err.NewMultiErrors()

	// Validate sender information
	if request.MailboxID == "" && request.FromAddress == "" {
		errs.Add("sender", "provide either a valid mailboxId or fromAddress", errors.New("invalid sender"))
	}

	// Validate recipients
	if len(request.ToAddresses) == 0 {
		errs.Add("toAddresses", "please provide at least one valid to address", errors.New("toAddresses is empty"))
	}

	// Validate subject and body
	if request.Subject == "" {
		errs.Add("subject", "please provide an email subject", errors.New("subject is empty"))
	}

	if request.Body.HTML == "" && request.Body.Text == "" {
		errs.Add("body", "please provide a valid html or text body (or both)", errors.New("body is empty"))
	}

	return errs
}

// buildEmailContainer creates an EmailContainer with validated data
func (h *EmailsHandler) buildSendEmailContainer(ctx context.Context, request *SendEmailRequest) (*EmailContainer, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "EmailsHandler.buildEmailContainer")
	defer span.Finish()

	// Create container
	container := &EmailContainer{}

	// Get and validate mailbox
	mailbox, err := h.resolveMailboxFromMailboxIDOrEmail(ctx, request)
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}
	container.Mailbox = mailbox

	// Validate outbound capability
	if !mailbox.OutboundEnabled {
		tracing.TraceErr(span, err)
		return nil, errors.New("mailbox not enabled for outbound email")
	}

	// Validate email addresses
	toAddresses, err := h.validateEmailAddresses(ctx, "toAddresses", request.ToAddresses)
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}

	ccAddresses, err := h.validateEmailAddresses(ctx, "ccAddresses", request.CCAddresses)
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}

	bccAddresses, err := h.validateEmailAddresses(ctx, "bccAddresses", request.BCCAddresses)
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}

	// Validate reply-to
	replyTo := request.ReplyTo
	if replyTo != "" {
		cleanReplyTo, err := h.validateEmailSyntax(ctx, replyTo)
		if err != nil {
			tracing.TraceErr(span, err)
			return nil, fmt.Errorf("invalid replyTo address: %w", err)
		}
		replyTo = cleanReplyTo
	}

	// Validate attachments
	attachments, err := h.resolveAttachments(ctx, request.Attachments)
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}
	container.Attachments = attachments

	// get sender profile
	senderProfile, err := h.repositories.SenderRepository.GetByID(ctx, mailbox.SenderID)
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}
	if senderProfile == nil {
		err := errors.New("No sender attached to mailbox")
		tracing.TraceErr(span, err)
		return nil, err
	}

	// Build the email model
	fromAddress := request.FromAddress
	if fromAddress == "" {
		fromAddress = mailbox.EmailAddress
	}

	fromName := request.FromName
	if fromName == "" {
		fromName = senderProfile.DisplayName
	}

	if replyTo == "" {
		replyTo = mailbox.ReplyToAddress
	}

	// Create email with validated data
	email := &models.Email{
		MailboxID:     mailbox.ID,
		Direction:     enum.EmailDirectionOutbound,
		MessageID:     utils.GenerateMessageID(mailbox.MailboxDomain, ""),
		Subject:       request.Subject,
		FromDomain:    mailbox.MailboxDomain,
		FromAddress:   fromAddress,
		FromUser:      strings.Split(fromAddress, "@")[0],
		FromName:      fromName,
		ToAddresses:   toAddresses,
		CcAddresses:   ccAddresses,
		BccAddresses:  bccAddresses,
		ReplyTo:       replyTo,
		BodyText:      request.Body.Text,
		BodyHTML:      request.Body.HTML,
		HasAttachment: len(attachments) > 0,
	}

	container.Email = email
	return container, nil
}

// resolveMailbox gets the mailbox from either ID or email address
func (h *EmailsHandler) resolveMailboxFromMailboxIDOrEmail(ctx context.Context, request *SendEmailRequest) (*models.Mailbox, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "EmailsHandler.resolveMailbox")
	defer span.Finish()

	if request.MailboxID != "" {
		// Try to get by ID first
		mailbox, err := h.repositories.MailboxRepository.GetMailbox(ctx, request.MailboxID)
		if err != nil {
			tracing.TraceErr(span, err)
			return nil, fmt.Errorf("unable to find mailbox with ID %s: %w", request.MailboxID, err)
		}
		if mailbox != nil {
			return mailbox, nil
		}
	}

	// If no mailbox found by ID or no ID provided, try by email address
	if request.FromAddress != "" {
		// Clean the email address first
		cleanEmail, err := h.validateEmailSyntax(ctx, request.FromAddress)
		if err != nil {
			return nil, fmt.Errorf("invalid fromAddress: %w", err)
		}

		mailbox, err := h.repositories.MailboxRepository.GetMailboxByEmailAddress(ctx, cleanEmail)
		if err != nil {
			tracing.TraceErr(span, err)
			return nil, fmt.Errorf("unable to find mailbox for email %s: %w", cleanEmail, err)
		}
		if mailbox != nil {
			return mailbox, nil
		}
	}

	return nil, errors.New("no valid mailbox found")
}

// validateEmailAddresses validates a list of email addresses
func (h *EmailsHandler) validateEmailAddresses(ctx context.Context, fieldName string, addresses []string) ([]string, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "EmailsHandler.validateEmailAddresses")
	defer span.Finish()

	if len(addresses) == 0 {
		return []string{}, nil
	}

	validAddresses := make([]string, 0, len(addresses))
	var invalidAddresses []string

	for _, address := range addresses {
		cleanAddress, err := h.validateEmailSyntax(ctx, address)
		if err != nil {
			invalidAddresses = append(invalidAddresses, address)
			continue
		}
		validAddresses = append(validAddresses, cleanAddress)
	}

	if len(invalidAddresses) > 0 {
		return nil, fmt.Errorf("invalid %s: %s", fieldName, strings.Join(invalidAddresses, ", "))
	}

	return validAddresses, nil
}

// processAndSendEmail creates a thread and sends the email
func (h *EmailsHandler) processAndSendEmail(ctx context.Context, container *EmailContainer) (*smtp.SendResult, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "EmailsHandler.processAndSendEmail")
	defer span.Finish()

	// Create a new email thread
	threadID, err := h.createEmailThread(ctx, container)
	if err != nil {
		return nil, fmt.Errorf("failed to create email thread: %w", err)
	}

	// Associate email with thread
	container.Email.ThreadID = threadID
	container.Email.Status = enum.EmailStatusQueued
	container.Email.SendAttempts = 1

	return h.sendEmail(ctx, container)
}

// createEmailThread creates a new email thread
func (h *EmailsHandler) createEmailThread(ctx context.Context, container *EmailContainer) (string, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "EmailsHandler.createEmailThread")
	defer span.Finish()

	thread := &models.EmailThread{
		MailboxID:      container.Mailbox.ID,
		Subject:        container.Email.Subject,
		Participants:   container.Email.AllParticipants(),
		LastMessageID:  container.Email.MessageID,
		HasAttachments: container.Email.HasAttachment,
		FirstMessageAt: utils.NowPtr(),
		LastMessageAt:  utils.NowPtr(),
	}

	threadID, err := h.repositories.EmailThreadRepository.Create(ctx, thread)
	if err != nil {
		tracing.TraceErr(span, err)
		return "", err
	}

	return threadID, nil
}
