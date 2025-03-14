package emails

import (
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

type ReplyEmailRequest struct {
	FromAddress  string        `json:"fromAddress"`
	ToAddresses  []string      `json:"toAddresses"`
	CCAddresses  []string      `json:"ccAddresses"`
	BCCAddresses []string      `json:"bccAddresses"`
	ReplyTo      string        `json:"replyTo"`
	Subject      string        `json:"subject"`
	Body         EmailBody     `json:"body"`
	Attachments  []Attachments `json:"attachments"`
	ScheduleFor  time.Time     `json:"scheduleFor"`
}

// reply to an existing email thread
func (h *EmailsHandler) Reply() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, span := tracing.StartHttpServerTracerSpanWithHeader(c.Request.Context(), "EmailsHandler.Send", c.Request.Header)
		defer span.Finish()
		tracing.TagComponentRest(span)
		tracing.TagTenant(span, utils.GetTenantFromContext(ctx))

		emailContainer, err := h.validateSendEmailRequest(c)
		if err != nil {
			tracing.TraceErr(span, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// create new email thread
		threadId, err := h.repositories.EmailThreadRepository.Create(ctx, &models.EmailThread{
			MailboxID:      emailContainer.Mailbox.ID,
			Subject:        emailContainer.Email.Subject,
			Participants:   emailContainer.Email.AllParticipants(),
			MessageCount:   1,
			LastMessageID:  emailContainer.Email.MessageID,
			HasAttachments: emailContainer.Email.HasAttachment,
			FirstMessageAt: utils.NowPtr(),
			LastMessageAt:  utils.NowPtr(),
		})
		if err != nil {
			tracing.TraceErr(span, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// save email record
		emailContainer.Email.ThreadID = threadId
		emailContainer.Email.Status = enum.EmailStatusQueued
		emailContainer.Email.SendAttempts = 1
		id, err := h.repositories.EmailRepository.Create(ctx, emailContainer.Email)
		if err != nil {
			tracing.TraceErr(span, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		emailContainer.Email.ID = id

		smtpClient := smtp.NewSMTPClient(h.repositories, emailContainer.Mailbox)

		results := smtpClient.Send(ctx, emailContainer.Email, emailContainer.Attachments)
		if !results.Success {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": results.ErrorMessage,
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"messageId": results.MessageID,
		})
	}
}

func (h *EmailsHandler) validateReplyEmailRequest(c *gin.Context) (EmailContainer, error) {
	span, ctx := opentracing.StartSpanFromContext(c.Request.Context(), "EmailsHandler.validateSendEmailRequest")
	defer span.Finish()
	tracing.TagComponentRest(span)

	errs := custom_err.NewMultiErrors()
	emailContainer := EmailContainer{}

	var emailData SendEmailRequest
	err := c.ShouldBindJSON(&emailData)
	if err != nil {
		tracing.TraceErr(span, err)
		errs.Add("request", "please provide a valid request payload", errors.New("cannot parse request"))
		return emailContainer, errs
	}

	if emailData.MailboxID == "" && emailData.FromAddress == "" {
		errs.Add("sender", "provide either a valid mailboxId or fromAddress", errors.New("Invalid sender"))
		tracing.TraceErr(span, err)
		return emailContainer, err
	}

	if emailData.MailboxID != "" {
		mailbox, err := h.repositories.MailboxRepository.GetMailbox(ctx, emailData.MailboxID)
		if err != nil || mailbox == nil {
			errs.Add("mailboxId", "unable to identify mailbox", err)
			tracing.TraceErr(span, err)
		}
		emailContainer.Mailbox = mailbox
	}

	if emailContainer.Mailbox == nil {
		err := h.validateEmailSyntax(ctx, &emailData.FromAddress)
		if err != nil {
			errs.Add("fromAddress", fmt.Sprintf("%s is invalid", emailData.FromAddress), err)
			tracing.TraceErr(span, err)
		}
		mailbox, err := h.repositories.MailboxRepository.GetMailboxByEmailAddress(ctx, emailData.FromAddress)
		if err != nil || mailbox == nil {
			errs.Add("mailboxId", "unable to identify mailbox", err)
			tracing.TraceErr(span, err)
		}
		emailContainer.Mailbox = mailbox
	}

	if !emailContainer.Mailbox.OutboundEnabled {
		err := errors.New("mailbox not enabled for outbound")
		tracing.TraceErr(span, err)
		errs.Add("mailbox", "please enable mailbox for sending", err)
	}

	if len(emailData.ToAddresses) == 0 {
		err := errors.New("toAddresses is empty")
		errs.Add("toAddresses", "please provide at least one valid to address", err)
		tracing.TraceErr(span, err)
	}

	emailData.ToAddresses = h.validateEmailAddresses(ctx, "toAddresses", emailData.ToAddresses, errs)
	emailData.CCAddresses = h.validateEmailAddresses(ctx, "ccAddresses", emailData.CCAddresses, errs)
	emailData.BCCAddresses = h.validateEmailAddresses(ctx, "bccAddresses", emailData.BCCAddresses, errs)

	if emailData.ReplyTo != "" {
		err := h.validateEmailSyntax(ctx, &emailData.ReplyTo)
		if err != nil {
			errs.Add("replyTo", fmt.Sprintf("%s is invalid", emailData.ReplyTo), err)
			tracing.TraceErr(span, err)
		}
	}

	if emailData.Subject == "" {
		errs.Add("subject", "please provide an email subject", errors.New("subject is empty"))
	}

	if emailData.Body.HTML == "" && emailData.Body.Text == "" {
		errs.Add("body", "please provide a valid html or text body (or both)", errors.New("body is empty"))
	}

	if len(emailData.Attachments) > 0 {
		emailContainer.Attachments = h.validateAttachments(ctx, emailData.Attachments, errs)
	}

	if errs.HasErrors() {
		return emailContainer, errs
	}

	// principle: data passed in api request takes priority over default vales stored on mailbox.
	// default values stored on mailbox will be used where data is not provided in api
	emailRecord := models.Email{
		MailboxID:    emailContainer.Mailbox.ID,
		Direction:    enum.EmailOutbound,
		MessageID:    utils.GenerateMessageID(emailContainer.Mailbox.MailboxDomain, ""),
		Subject:      emailData.Subject,
		FromDomain:   emailContainer.Mailbox.MailboxDomain,
		FromAddress:  emailData.FromAddress,
		FromUser:     strings.Split(emailData.FromAddress, "@")[0],
		FromName:     emailData.FromName,
		ToAddresses:  emailData.ToAddresses,
		CcAddresses:  emailData.CCAddresses,
		BccAddresses: emailData.BCCAddresses,
		ReplyTo:      emailData.ReplyTo,
		BodyText:     emailData.Body.Text,
		BodyHTML:     emailData.Body.HTML,
	}

	if emailRecord.FromAddress == "" {
		emailRecord.FromAddress = emailContainer.Mailbox.EmailAddress
	}
	if emailRecord.FromName == "" {
		emailRecord.FromName = emailContainer.Mailbox.DefaultFromName
	}
	if emailRecord.ReplyTo == "" && emailContainer.Mailbox.ReplyToAddress == "" {
		emailRecord.ReplyTo = emailContainer.Mailbox.ReplyToAddress
	}
	if len(emailContainer.Attachments) > 0 {
		emailRecord.HasAttachment = true
	}

	emailContainer.Email = &emailRecord

	return emailContainer, nil
}
