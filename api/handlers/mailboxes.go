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
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"
	"github.com/customeros/mailstack/services"
)

type MailboxHandler struct {
	repos          *repository.Repositories
	cfg            *config.Config
	mailboxService interfaces.MailboxService
	services       *services.Services
}

func NewMailboxHandler(repos *repository.Repositories, cfg *config.Config, s *services.Services) *MailboxHandler {
	return &MailboxHandler{
		repos:          repos,
		cfg:            cfg,
		mailboxService: s.MailboxService,
		services:       s,
	}
}

type NewMailboxRequest struct {
	Username              string   `json:"username"`
	Password              string   `json:"password"`
	Domain                string   `json:"domain"`
	ForwardingTo          []string `json:"forwardingTo"`
	WebmailEnabled        bool     `json:"webmailEnabled"`
	UserId                string   `json:"userId"`
	IgnoreDomainOwnership bool     `json:"ignoreDomainOwnership"`
}

type MailboxesResponse struct {
	Mailboxes []MailboxRecord `json:"mailboxes,omitempty"`
}

type MailboxRecord struct {
	ID                string   `json:"id,omitempty"`
	Email             string   `json:"email"`
	Password          string   `json:"password,omitempty"`
	ForwardingEnabled bool     `json:"forwardingEnabled"`
	ForwardingTo      []string `json:"forwardingTo"`
	WebmailEnabled    bool     `json:"webmailEnabled"`
	Provisioned       bool     `json:"provisioned"`
}

func (h *MailboxHandler) GetMailboxes() gin.HandlerFunc {
	return func(c *gin.Context) {
		span, ctx := opentracing.StartSpanFromContext(c.Request.Context(), "MailboxHandler.GetMailboxes")
		defer span.Finish()
		tracing.SetDefaultRestSpanTags(ctx, span)

		// get domain from path params
		domain, _ := c.GetQuery("domain")

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
			mailboxDetails, err := h.services.OpenSrsService.GetMailboxDetails(ctx, mailboxRecord.MailboxUsername)
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
				Provisioned:       mailboxRecord.Status == models.MailboxStatusProvisioned,
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
			Domain:                domain,
			Username:              username,
			Password:              password,
			UserId:                request.UserId,
			WebmailEnabled:        request.WebmailEnabled,
			ForwardingTo:          forwardingTo,
			IgnoreDomainOwnership: request.IgnoreDomainOwnership,
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
			ID:                mailbox.ID,
			Email:             username + "@" + domain,
			WebmailEnabled:    request.WebmailEnabled,
			ForwardingEnabled: true,
			ForwardingTo:      forwardingTo,
			Provisioned:       mailbox.Status == models.MailboxStatusProvisioned,
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

func (h *MailboxHandler) ConfigureMailbox() gin.HandlerFunc {
	return func(c *gin.Context) {
		span, ctx := opentracing.StartSpanFromContext(c.Request.Context(), "MailboxHandler.ConfigureMailbox")
		defer span.Finish()
		tracing.SetDefaultRestSpanTags(ctx, span)

		tenant := utils.GetTenantFromContext(ctx)

		mailboxID := c.Param("id")
		if mailboxID == "" {
			tracing.TraceErr(span, errors.New("mailbox ID is required"))
			c.JSON(http.StatusBadRequest, gin.H{"error": "mailbox ID is required"})
			return
		}

		// Get the mailbox from the repository
		mailbox, err := h.repos.TenantSettingsMailboxRepository.GetById(ctx, mailboxID)
		if err != nil {
			tracing.TraceErr(span, errors.Wrap(err, "failed to get mailbox"))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get mailbox"})
			return
		}
		if mailbox == nil {
			tracing.TraceErr(span, errors.New("mailbox not found"))
			c.JSON(http.StatusNotFound, gin.H{"error": "mailbox not found"})
			return
		}

		// Verify tenant ownership
		if mailbox.Tenant != tenant {
			tracing.TraceErr(span, errors.New("mailbox does not belong to tenant"))
			c.JSON(http.StatusForbidden, gin.H{"error": "mailbox does not belong to tenant"})
			return
		}

		// Call OpenSRS to configure the mailbox
		forwardingTo := []string{}
		if mailbox.ForwardingTo != "" {
			forwardingTo = strings.Split(mailbox.ForwardingTo, ",")
		}

		err = h.services.OpenSrsService.SetupMailbox(ctx, tenant, mailbox.MailboxUsername, mailbox.MailboxPassword, forwardingTo, mailbox.WebmailEnabled)
		if err != nil {
			tracing.TraceErr(span, errors.Wrap(err, "failed to configure mailbox with OpenSRS"))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to configure mailbox with OpenSRS"})
			return
		}

		// Update mailbox status to provisioned
		err = h.repos.TenantSettingsMailboxRepository.UpdateStatus(ctx, mailboxID, models.MailboxStatusProvisioned)
		if err != nil {
			tracing.TraceErr(span, errors.Wrap(err, "failed to update mailbox status"))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update mailbox status"})
			return
		}

		c.JSON(http.StatusOK, gin.H{})
	}
}
