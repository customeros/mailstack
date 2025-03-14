package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/config"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/services"
)

type MailboxHandler struct {
	repos          *repository.Repositories
	cfg            *config.Config
	mailboxService interfaces.MailboxService
	openSrsService interfaces.OpenSrsService
}

func NewMailboxHandler(repos *repository.Repositories, cfg *config.Config, s *services.Services) *MailboxHandler {
	return &MailboxHandler{
		repos:          repos,
		cfg:            cfg,
		mailboxService: s.MailboxService,
		openSrsService: s.OpenSrsService,
	}
}

type MailboxesResponse struct {
	Mailboxes []MailboxRecord `json:"mailboxes,omitempty"`
}

type MailboxRecord struct {
	Email             string   `json:"email"`
	Password          string   `json:"password,omitempty"`
	ForwardingEnabled bool     `json:"forwardingEnabled"`
	ForwardingTo      []string `json:"forwardingTo"`
	WebmailEnabled    bool     `json:"webmailEnabled"`
}

func (h *MailboxHandler) GetMailboxes() gin.HandlerFunc {
	return func(c *gin.Context) {
		span, ctx := opentracing.StartSpanFromContext(c.Request.Context(), "MailboxHandler.GetMailboxes")
		defer span.Finish()
		tracing.SetDefaultRestSpanTags(ctx, span)

		// get domain from path params
		domain := c.Param("domain")

		mailboxRecords, err := h.mailboxService.GetMailboxes(ctx, domain)
		if err != nil {
			tracing.TraceErr(span, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		response := MailboxesResponse{
			Mailboxes: make([]MailboxRecord, 0, len(mailboxRecords)),
		}
		for _, mailboxRecord := range mailboxRecords {
			mailboxDetails, err := h.openSrsService.GetMailboxDetails(ctx, mailboxRecord.MailboxUsername)
			if err != nil {
				tracing.TraceErr(span, errors.Wrap(err, "Could not get mailbox details"))
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			response.Mailboxes = append(response.Mailboxes, MailboxRecord{
				Email:             mailboxRecord.MailboxUsername,
				ForwardingEnabled: mailboxDetails.ForwardingEnabled,
				ForwardingTo:      mailboxDetails.ForwardingTo,
				WebmailEnabled:    mailboxDetails.WebmailEnabled,
			})
		}

		c.JSON(http.StatusOK, response)
	}
}

// // ListMailboxes returns all configured mailboxes
// func ListMailboxes(imapService interfaces.IMAPService) gin.HandlerFunc {
// 	return func(c *gin.Context) {
// 		// This would need to be implemented - for now just use the status info
// 		status := imapService.Status()
// 		c.JSON(http.StatusOK, status)
// 	}
// }

// // AddMailbox adds a new mailbox configuration
// func AddMailbox(imapService interfaces.IMAPService, mailboxRepository interfaces.MailboxRepository) gin.HandlerFunc {
// 	return func(c *gin.Context) {
// 		ctx, span := tracing.StartHttpServerTracerSpanWithHeader(c.Request.Context(), "AddMailbox", c.Request.Header)
// 		defer span.Finish()
// 		tracing.TagComponentRest(span)
// 		tracing.TagTenant(span, utils.GetTenantFromContext(ctx))

// 		var config models.Mailbox
// 		err := c.ShouldBindJSON(&config)
// 		if err != nil {
// 			tracing.TraceErr(span, err)
// 			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
// 			return
// 		}

// 		err = mailboxRepository.SaveMailbox(ctx, config)
// 		if err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
// 			return
// 		}

// 		if config.InboundEnabled == true {
// 			err = imapService.AddMailbox(ctx, &config)
// 			if err != nil {
// 				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
// 				return
// 			}
// 		}

// 		c.JSON(http.StatusCreated, gin.H{"status": "mailbox added", "id": config.ID})
// 	}
// }

// // RemoveMailbox removes a mailbox configuration
// func RemoveMailbox(imapService interfaces.IMAPService) gin.HandlerFunc {
// 	return func(c *gin.Context) {
// 		ctx, span := tracing.StartHttpServerTracerSpanWithHeader(c.Request.Context(), "RemoveMailbox", c.Request.Header)
// 		defer span.Finish()
// 		tracing.TagComponentRest(span)
// 		tracing.TagTenant(span, utils.GetTenantFromContext(ctx))

// 		id := c.Param("id")
// 		if err := imapService.RemoveMailbox(ctx, id); err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
// 			return
// 		}

// 		c.JSON(http.StatusOK, gin.H{"status": "mailbox removed", "id": id})
// 	}
// }
