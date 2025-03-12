package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"
	"github.com/customeros/mailstack/services/smtp"
)

// ListMailboxes returns all configured mailboxes
func Send(r *repository.Repositories) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, span := tracing.StartHttpServerTracerSpanWithHeader(c.Request.Context(), "Send", c.Request.Header)
		defer span.Finish()
		tracing.TagComponentRest(span)
		tracing.TagTenant(span, utils.GetTenantFromContext(ctx))

		var email models.Email
		err := c.ShouldBindJSON(&email)
		if err != nil {
			tracing.TraceErr(span, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		mailbox, err := r.MailboxRepository.GetMailbox(ctx, email.MailboxID)
		if err != nil {
			tracing.TraceErr(span, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		smtpClient := smtp.NewSMTPClient(r, mailbox)
		results := smtpClient.Send(ctx, &email, nil)

		c.JSON(http.StatusOK, results)
		return
	}
}
