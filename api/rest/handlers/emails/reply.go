package emails

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"

	custom_err "github.com/customeros/mailstack/api/errors"
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"
)

type ReplyEmailRequest struct {
	ReplyTo     string        `json:"replyTo"`
	Body        EmailBody     `json:"body"`
	Attachments []Attachments `json:"attachments"`
	ScheduleFor time.Time     `json:"scheduleFor"`
}

// reply to an existing email thread
func (h *EmailsHandler) Reply() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, span := tracing.StartHttpServerTracerSpanWithHeader(c.Request.Context(), "EmailsHandler.Reply", c.Request.Header)
		defer span.Finish()
		tracing.TagComponentRest(span)
		tracing.TagTenant(span, utils.GetTenantFromContext(ctx))

		// Parse the request
		var request ReplyEmailRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			h.respondWithError(c, span, http.StatusBadRequest, "Invalid request format", err)
			return
		}

		// validate email exists
		replyToEmailID := c.Param("id")
		if replyToEmailID == "" {
			h.respondWithError(c, span, http.StatusBadRequest, "emailID cannot be empty", nil)
			return
		}
		replyToEmail, err := h.repositories.EmailRepository.GetByID(ctx, replyToEmailID)
		if err != nil {
			h.respondWithError(c, span, http.StatusInternalServerError, "error retrieving email", err)
			return
		}
		if replyToEmail == nil {
			h.respondWithError(c, span, http.StatusNotFound, fmt.Sprintf("cannot find email with id %s", replyToEmailID), nil)
			return
		}

		// Validate request
		errs := h.ValidateReplyRequest(ctx, &request)
		if errs.HasErrors() {
			tracing.TraceErr(span, errs)
			c.JSON(http.StatusBadRequest, errs)
		}

		// Build email container
		emailContainer, err := h.buildReplyEmailContainer(ctx, &request, replyToEmail)
		if err != nil {
			h.respondWithError(c, span, http.StatusInternalServerError, "error retrieving email", err)
			return
		}
		if emailContainer == nil {
			h.respondWithError(c, span, http.StatusNotFound, fmt.Sprintf("cannot find email thread with id %s", replyToEmailID), nil)
			return
		}

		// Send reply
		result, err := h.sendEmail(ctx, emailContainer)
		if err != nil {
			h.respondWithError(c, span, http.StatusInternalServerError, "Failed to send email", err)
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"messageId": result.MessageID,
		})
	}
}

func (h *EmailsHandler) ValidateReplyRequest(ctx context.Context, request *ReplyEmailRequest) *custom_err.MultiErrors {
	span, ctx := opentracing.StartSpanFromContext(ctx, "EmailsHandler.validateRequest")
	defer span.Finish()
	tracing.TagComponentRest(span)

	var errs custom_err.MultiErrors
	if request.Body.HTML == "" && request.Body.Text == "" {
		errs.Add("body", "please provide a valid html or text body (or both)", errors.New("body is empty"))
	}
	return &errs
}

func (h *EmailsHandler) buildReplyEmailContainer(ctx context.Context, request *ReplyEmailRequest, replyToEmail *models.Email) (*EmailContainer, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "EmailsHandler.buildReplyEmailContainer")
	defer span.Finish()
	tracing.TagComponentRest(span)

	// Create container
	container := &EmailContainer{}

	// Get mailbox
	mailbox, err := h.repositories.MailboxRepository.GetMailbox(ctx, replyToEmail.MailboxID)
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
	if senderProfile.DisplayName == "" {
		senderProfile.DisplayName = mailbox.EmailAddress
	}

	// build email model
	email := &models.Email{
		MailboxID:    mailbox.ID,
		Direction:    enum.EmailDirectionOutbound,
		MessageID:    utils.GenerateMessageID(mailbox.MailboxDomain, ""),
		ThreadID:     replyToEmail.ThreadID,
		InReplyTo:    replyToEmail.MessageID,
		Subject:      fmt.Sprintf("RE: %s", replyToEmail.CleanSubject),
		FromAddress:  mailbox.EmailAddress,
		FromName:     senderProfile.DisplayName,
		FromUser:     mailbox.MailboxUser,
		FromDomain:   mailbox.MailboxDomain,
		ReplyTo:      replyTo,
		ToAddresses:  []string{replyToEmail.FromAddress},
		CcAddresses:  []string{},
		BccAddresses: []string{},
		BodyText:     request.Body.Text,
		BodyHTML:     request.Body.HTML,
		Status:       enum.EmailStatusQueued,
		SendAttempts: 1,
	}

	// if reply-to header set on replyToEmail, use reply-to address as to
	if replyToEmail.ReplyTo != "" {
		replyTo := []string{replyToEmail.ReplyTo}
		email.ToAddresses = replyTo
	}

	if len(request.Attachments) > 0 {
		email.HasAttachment = true
	}

	// build references from all previous emails in thread
	threadRecord, err := h.repositories.EmailRepository.ListByThread(ctx, replyToEmail.ThreadID)
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}
	if threadRecord == nil {
		err := errors.New("unable to identify email thread")
		tracing.TraceErr(span, err)
		return nil, err
	}

	for _, e := range threadRecord {
		messageID := fmt.Sprintf("<%s>", e.MessageID)
		if !utils.IsStringInSlice(messageID, email.References) {
			email.References = append(email.References, messageID)
		}
	}

	container.Email = email
	return container, nil
}
