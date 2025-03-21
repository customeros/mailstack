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
	"github.com/customeros/mailstack/internal"
	"github.com/customeros/mailstack/internal/config"
	"github.com/customeros/mailstack/internal/listeners"
	"github.com/customeros/mailstack/internal/logger"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/services"
	"github.com/customeros/mailstack/services/events"
)

type Server struct {
	config       *config.Config
	logger       logger.Logger
	tracerCloser io.Closer
	httpServer   *http.Server
	router       *gin.Engine
	services     *services.Services
	repositories *repository.Repositories
}

func NewServer(cfg *config.Config, mailstackDB *gorm.DB, openlineDB *gorm.DB) (*Server, error) {
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
	repos := repository.InitRepositories(mailstackDB, openlineDB, cfg.R2StorageConfig)

	// Initialize services
	svcs, err := services.InitServices(cfg.AppConfig.RabbitMQURL, logger, repos, cfg)
	if err != nil {
		return nil, err
	}

	// Initialize listeners
	svcs.EventsService.Subscriber.RegisterListener(listeners.NewSendEmailListener(logger, repos, svcs.EmailService))
	svcs.EventsService.Subscriber.RegisterListener(listeners.NewReceiveEmailListener(logger, repos, svcs.IMAPProcessor))

	// Start Listening on rabbit queues
	err = svcs.EventsService.Subscriber.ListenQueue(events.QueueSendEmail)
	if err != nil {
		logger.Errorf("Failed to start listening on send email queue: %v", err)
	}
	err = svcs.EventsService.Subscriber.ListenQueue(events.QueueReceiveEmail)
	if err != nil {
		logger.Errorf("Failed to start listening on receive email queue: %v", err)
	}

	// Initialize Gin
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()

	return &Server{
		config:       cfg,
		router:       router,
		services:     svcs,
		repositories: repos,
		tracerCloser: closer,
		httpServer: &http.Server{
			Addr:    ":" + cfg.AppConfig.APIPort,
			Handler: router,
		},
		logger: logger,
	}, nil
}

func (s *Server) Initialize(ctx context.Context) error {
	// Register webhook handler
	log.Println("Registering event handler...")

	// Setup mailboxes
	if err := internal.InitMailboxes(s.services, s.repositories); err != nil {
		return err
	}

	// Setup API routes
	api.RegisterRoutes(ctx, s.router, s.services, s.repositories, s.config)

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

func (s *Server) Logger() logger.Logger {
	return s.logger
}

func (s *Server) Services() *services.Services {
	return s.services
}

func (s *Server) Repositories() *repository.Repositories {
	return s.repositories
}
