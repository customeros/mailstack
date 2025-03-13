package server

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"gorm.io/gorm"

	"github.com/customeros/mailstack/api"
	"github.com/customeros/mailstack/config"
	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal"
	"github.com/customeros/mailstack/internal/logger"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/services"
	"github.com/customeros/mailstack/services/email_processor"
)

type Server struct {
	config         *config.Config
	httpServer     *http.Server
	router         *gin.Engine
	services       *services.Services
	repositories   *repository.Repositories
	emailProcessor *email_processor.Processor
	tracerCloser   io.Closer
}

func NewServer(cfg *config.Config, mailstackDB *gorm.DB) (*Server, error) {
	// Initialize logger
	logger := logger.NewAppLogger(cfg.Logger)
	logger.InitLogger()

	// Initialize tracing
	tracer, closer, err := tracing.NewJaegerTracer(cfg.Tracing, logger)
	if err != nil {
		log.Fatalf("Could not initialize jaeger tracer: %s", err.Error())
	}
	opentracing.SetGlobalTracer(tracer)

	// Initialize repositories
	repos := repository.InitRepositories(mailstackDB, cfg.R2StorageConfig)

	// Initialize services
	svcs, err := services.InitServices(cfg.AppConfig.RabbitMQURL, logger, repos)
	if err != nil {
		return nil, err
	}

	// Set up handler for email events
	emailProcessor := email_processor.NewProcessor(repos, svcs.EventsService, svcs.EmailFilterService)

	// Initialize Gin
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()

	return &Server{
		config:         cfg,
		router:         router,
		services:       svcs,
		repositories:   repos,
		emailProcessor: emailProcessor,
		tracerCloser:   closer,
		httpServer: &http.Server{
			Addr:    ":" + cfg.AppConfig.APIPort,
			Handler: router,
		},
	}, nil
}

func (s *Server) Initialize(ctx context.Context) error {
	// Register webhook handler
	log.Println("Registering event handler...")

	// Create an adapter function that wraps ProcessMailEvent with panic recovery
	eventHandler := func(ctx context.Context, mailEvent interfaces.MailEvent) {
		s.wrapGoroutine("event_handler", func() {
			s.emailProcessor.ProcessMailEvent(ctx, mailEvent)
		})
	}

	s.services.IMAPService.SetEventHandler(eventHandler)

	// Setup mailboxes
	if err := internal.InitMailboxes(s.services, s.repositories); err != nil {
		return err
	}

	// Setup API routes
	api.RegisterRoutes(ctx, s.router, s.services, s.repositories, s.config.AppConfig.APIKey)

	return nil
}

func (s *Server) recoverWithJaeger(name string) {
	if r := recover(); r != nil {
		// Create a new span for the panic
		span := opentracing.GlobalTracer().StartSpan(
			fmt.Sprintf("panic.%s", name),
		)
		defer span.Finish()

		// Mark span as failed
		ext.Error.Set(span, true)

		// Log panic details
		span.LogKV(
			"event", "panic",
			"process", name,
			"error", fmt.Sprintf("%v", r),
			"stack", string(debug.Stack()),
		)

		log.Printf("❌ Panic in %s: %v\n%s", name, r, debug.Stack())
	}
}

func (s *Server) wrapGoroutine(name string, fn func()) {
	defer s.recoverWithJaeger(name)
	fn()
}

func (s *Server) Run() error {
	// Create root context for the application
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize server components
	if err := s.Initialize(ctx); err != nil {
		return err
	}

	// Start the IMAP service with panic recovery
	log.Println("Starting IMAP service...")
	s.wrapGoroutine("imap_service", func() {
		if err := s.services.IMAPService.Start(ctx); err != nil {
			log.Printf("❌ IMAP service error: %v", err)
		}
	})
	log.Println("✅ IMAP service started successfully")

	// Start HTTP server in a goroutine with panic recovery
	go s.wrapGoroutine("http_server", func() {
		log.Println("Starting HTTP server")
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("❌ HTTP server error: %v", err)
		}
	})
	log.Println("✅ HTTP server started successfully")
	log.Println("MailStack is now running. Press Ctrl+C to exit.")

	return s.waitForShutdown()
}

func (s *Server) waitForShutdown() error {
	defer s.recoverWithJaeger("shutdown")

	// Set up signal handling for graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Wait for termination signal
	<-stop
	log.Println("Shutting down...")

	// Create a context with timeout for shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	// Shut down HTTP server
	log.Println("Shutting down HTTP server...")
	if s.tracerCloser != nil {
		s.tracerCloser.Close()
	}

	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("❌ HTTP server shutdown error: %v", err)
	} else {
		log.Println("✅ HTTP server shut down successfully")
	}

	// Stop IMAP service with timeout and panic recovery
	log.Println("Stopping IMAP service...")
	stopDone := make(chan struct{})
	go s.wrapGoroutine("imap_service_shutdown", func() {
		defer close(stopDone)
		if err := s.services.IMAPService.Stop(); err != nil {
			log.Printf("❌ IMAP service shutdown error: %v", err)
		} else {
			log.Println("✅ IMAP service stopped successfully")
		}
	})

	// Wait for IMAP service to stop with timeout
	select {
	case <-stopDone:
		log.Println("IMAP service stopped gracefully")
	case <-time.After(10 * time.Second):
		log.Println("⚠️ IMAP service stop timed out, forcing exit")
	}

	return nil
}
