package handlers

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/opentracing/opentracing-go"
	tracingLog "github.com/opentracing/opentracing-go/log"
	"github.com/pkg/errors"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/config"
	er "github.com/customeros/mailstack/internal/errors"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"
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

type NewMailboxRequest struct {
	Username       string   `json:"username"`
	Password       string   `json:"password"`
	Domain         string   `json:"domain"`
	ForwardingTo   []string `json:"forwardingTo"`
	WebmailEnabled bool     `json:"webmailEnabled"`
	UserId         string   `json:"userId"`
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

func (h *MailboxHandler) RegisterNewMailbox() gin.HandlerFunc {
	return func(c *gin.Context) {
		span, ctx := opentracing.StartSpanFromContext(c.Request.Context(), "MailboxHandler.RegisterNewMailbox")
		defer span.Finish()
		tracing.SetDefaultRestSpanTags(ctx, span)

		tenant := utils.GetTenantFromContext(ctx)

		// Parse and validate request body
		var request NewMailboxRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			tracing.TraceErr(span, errors.Wrap(err, "Invalid request body"))
			// log body
			body, _ := c.GetRawData()
			span.LogFields(tracingLog.String("request.body", string(body)))
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		tracing.LogObjectAsJson(span, "request", request)

		// validate domain
		if request.Domain == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Domain is required"})
			return
		}
		domain := strings.TrimSpace(request.Domain)

		username := strings.TrimSpace(request.Username)
		if username == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Username is required"})
			return
		}

		password := strings.TrimSpace(request.Password)
		passwordGenerated := false
		if password == "" {
			passwordGenerated = true
			password = utils.GenerateLowerAlpha(1) + utils.GenerateKey(11, false)
		}

		// validate username format
		if err := validateMailboxUsername(username); err != nil {
			message := "username has wrong format"
			tracing.TraceErr(span, errors.Wrap(err, message))
			c.JSON(http.StatusBadRequest, gin.H{"error": message})
			return
		}

		// add bcc forwarding address
		forwardingTo := request.ForwardingTo
		additionalForwardingTo := fmt.Sprintf("bcc@%s.customeros.ai", strings.ToLower(tenant))
		forwardingTo = append(forwardingTo, additionalForwardingTo)

		err := h.mailboxService.CreateMailbox(ctx, nil, interfaces.CreateMailboxRequest{
			Domain:         domain,
			Username:       username,
			Password:       password,
			UserId:         request.UserId,
			WebmailEnabled: request.WebmailEnabled,
			ForwardingTo:   forwardingTo,
		})
		if err != nil {
			if errors.Is(err, er.ErrDomainNotFound) {
				message := "domain not found"
				tracing.TraceErr(span, errors.Wrap(err, message))
				c.JSON(http.StatusNotFound, gin.H{"error": message})
				return
			} else if errors.Is(err, er.ErrMailboxExists) {
				message := "username already exists"
				tracing.TraceErr(span, errors.Wrap(err, message))
				c.JSON(http.StatusConflict, gin.H{"error": message})
				return
			} else {
				message := "Mailbox setup failed"
				tracing.TraceErr(span, errors.Wrap(err, message))
				c.JSON(http.StatusInternalServerError, gin.H{"error": message})
				return
			}
		}

		mailbox, err := h.mailboxService.GetByMailbox(ctx, username, domain)
		if err != nil {
			tracing.TraceErr(span, errors.Wrap(err, "Error retrieving mailbox"))
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if mailbox == nil {
			tracing.TraceErr(span, errors.New("mailbox not created"))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "mailbox not created"})
			return
		}

		response := MailboxRecord{
			Email:             username + "@" + domain,
			WebmailEnabled:    request.WebmailEnabled,
			ForwardingEnabled: true,
			ForwardingTo:      forwardingTo,
		}

		if passwordGenerated {
			response.Password = password
		}
		c.JSON(http.StatusCreated, response)
	}
}

func validateMailboxUsername(username string) error {
	// Regular expression for a valid username (allows alphanumeric, dots, underscores, hyphens)
	re := regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	if !re.MatchString(username) {
		return errors.New("invalid username format: only alphanumeric characters, dots, underscores, and hyphens are allowed")
	}
	// Additional checks (length, etc.) can be added if necessary
	return nil
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
