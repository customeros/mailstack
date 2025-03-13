package api

import (
	"context"

	"github.com/customeros/mailstack/services"

	"github.com/gin-gonic/gin"
	"github.com/opentracing/opentracing-go"

	"github.com/customeros/mailstack/api/handlers"
	"github.com/customeros/mailstack/api/middleware"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/tracing"
)

// RegisterRoutes sets up all API endpoints
func RegisterRoutes(ctx context.Context, r *gin.Engine, s *services.Services, repos *repository.Repositories, apikey string) {
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
	apiHandlers := handlers.InitHandlers(repos)

	// Health check and status endpoints (no custom context needed)
	r.GET("/health", handlers.HealthCheck)
	r.GET("/status", handlers.Status(s.IMAPService))

	apiKeyMiddleware := middleware.APIKeyMiddleware(middleware.APIKeyConfig{
		HeaderName:  "X-CUSTOMER-OS-API-KEY",
		ValidAPIKey: apikey,
	})

	// API group with version and custom context
	api := r.Group("/v1")
	api.Use(apiKeyMiddleware)
	api.Use(middleware.CustomContextMiddleware("mailstack")) // Add custom context for all /v1/* endpoints
	api.Use(middleware.TracingMiddleware())                  // Add tracing for all /v1/* endpoints
	{
		// Mailbox endpoints
		mailboxes := api.Group("/mailboxes")
		{
			mailboxes.GET("", handlers.ListMailboxes(s.IMAPService))
			mailboxes.POST("", handlers.AddMailbox(s.IMAPService, repos.MailboxRepository))
			mailboxes.DELETE("/:id", handlers.RemoveMailbox(s.IMAPService))
		}

		// Email endpoints
		emails := api.Group("/emails")
		{
			emails.POST("", apiHandlers.Emails.Send()) // send
			emails.GET("/:id", nil)                    // get specific email
			emails.POST("/:id/reply", nil)             // reply to an email
			emails.POST("/:id/replyall", nil)          // reply-all to an email
			emails.POST("/:id/forward", nil)           // forward an email
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
