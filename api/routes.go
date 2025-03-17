package api

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/opentracing/opentracing-go"

	"github.com/customeros/mailstack/api/handlers"
	"github.com/customeros/mailstack/api/middleware"
	"github.com/customeros/mailstack/internal/config"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/services"
)

// RegisterRoutes sets up all API endpoints
func RegisterRoutes(ctx context.Context, r *gin.Engine, s *services.Services, repos *repository.Repositories, cfg *config.Config) {
	if s == nil {
		panic("Services cannot be nil")
	}
	if repos == nil {
		panic("Repositories cannot be nil")
	}

	// Add recovery middlewares
	r.Use(gin.Recovery())                                         // Gin's built-in recovery
	r.Use(tracing.RecoveryWithJaeger(opentracing.GlobalTracer())) // Our custom Jaeger recovery

	// setup handlers
	apiHandlers := handlers.InitHandlers(repos, cfg, s)

	// Health check and status endpoints (no custom context needed)
	r.GET("/health", handlers.HealthCheck)
	r.GET("/status", handlers.Status(s.IMAPService))

	apiKeyMiddleware := middleware.APIKeyMiddleware(middleware.APIKeyConfig{
		HeaderName:  "X-CUSTOMER-OS-API-KEY",
		ValidAPIKey: cfg.AppConfig.APIKey,
	})

	// API group with version and custom context
	api := r.Group("/v1")
	api.Use(apiKeyMiddleware)
	api.Use(middleware.CustomContextMiddleware("mailstack")) // Add custom context
	api.Use(middleware.TracingMiddleware(ctx))               // Add tracing with parent context
	{
		// Domain endpoints
		domains := api.Group("/domains")
		domains.Use(middleware.TenantValidationMiddleware()) // Tenant validation for domains
		{
			// Domain discovery and acquisition
			domains.GET("/check-availability/:domain", apiHandlers.Domains.CheckAvailability())
			domains.GET("/recommendations", apiHandlers.Domains.GetRecommendations())

			// Domain registration and configuration
			domains.POST("/purchase", apiHandlers.Domains.PurchaseDomain())
			domains.POST("/configure", apiHandlers.Domains.ConfigureDomain())
			domains.POST("", apiHandlers.Domains.RegisterNewDomain()) // Combined purchase + configure

			// DNS management
			domains.POST("/:domain/dns", apiHandlers.DNS.AddDNSRecord())
			domains.GET("/:domain/dns", apiHandlers.DNS.GetDNSRecords())
			domains.DELETE("/:domain/dns/:id", apiHandlers.DNS.DeleteDNSRecord())

			// Domain listing
			domains.GET("", apiHandlers.Domains.GetDomains())
		}

		// Mailbox endpoints
		mailboxes := api.Group("/mailboxes")
		mailboxes.Use(middleware.TenantValidationMiddleware())
		{
			mailboxes.GET("", apiHandlers.Mailbox.GetMailboxes())
			mailboxes.POST("", apiHandlers.Mailbox.RegisterNewMailbox())
			mailboxes.POST("/:id/configure", apiHandlers.Mailbox.ConfigureMailbox())
			// delete mailbox
			// mailboxes.DELETE("/:id", apiHandlers.Mailbox.RemoveMailbox())
		}

		// Dmarc endpoints
		dmarc := api.Group("/dmarc")
		{
			dmarc.POST("", apiHandlers.Postmark.PostmarkDMARCMonitor())
		}

		// Email endpoints
		emails := api.Group("/emails")
		{
			emails.POST("", apiHandlers.Emails.Send())            // send
			emails.GET("/:id", nil)                               // get specific email
			emails.POST("/:id/reply", apiHandlers.Emails.Reply()) // reply to an email
			emails.POST("/:id/replyall", nil)                     // reply-all to an email
			emails.POST("/:id/forward", nil)                      // forward an email
		}

		attachments := api.Group("/attachments")
		{
			attachments.POST("", nil)    // upload attachment, get id to use in email
			attachments.GET("/:id", nil) // get attachment
		}

		drafts := api.Group("/drafts")
		{
			drafts.POST("", nil)          // create a new draft
			drafts.GET("", nil)           // list all drafts
			drafts.GET("/:id", nil)       // get a draft
			drafts.PUT("/:id", nil)       // update a draft
			drafts.DELETE("/:id", nil)    // delete a draft
			drafts.POST("/:id/send", nil) // send a draft
		}
	}
}
