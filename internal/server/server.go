package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/customeros/mailstack/api"
	"github.com/customeros/mailstack/internal/config"
	"github.com/customeros/mailstack/internal/cron"
	"github.com/customeros/mailstack/internal/logger"
	nats_internal "github.com/customeros/mailstack/internal/nats"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/telemetry"
	"github.com/customeros/mailstack/services"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type Server struct {
	config       *config.Config
	logger       logger.Logger
	httpServer   *http.Server
	cronMgr      *cron.CronManager
	router       *gin.Engine
	natsConn     *nats_internal.NATSConnections
	services     *services.Services
	repositories *repository.Repositories
}

func NewServer(cfg *config.Config, mailstackDB *gorm.DB, warehouseDB *gorm.DB) (*Server, error) {
	// Initialize logger
	appLogger := logger.NewAppLogger(cfg.Logger)
	appLogger.InitLogger()

	// Initialize OpenTelemetry
	err := telemetry.InitOpenTelemetry(context.Background(), cfg.OpenTelemetry)
	if err != nil {
		log.Printf("Warning: Could not initialize OpenTelemetry: %s", err.Error())
	}

	// Initialize repositories
	repos := repository.InitRepositories(mailstackDB, warehouseDB, cfg.R2StorageConfig)
	if err != nil {
		return nil, err
	}

	// Initialize NATS Streams
	natsConn, err := nats_internal.InitNats(cfg.NATSConfig, cfg.AppConfig.Environment)
	if err != nil {
		log.Fatalf("Failed to initialize NATS: %v", err)
	}

	// Initialize services
	svcs := services.InitServices(natsConn, appLogger, repos, cfg)

	// Initialize Gin
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()

	// Try to get Kubernetes config
	var k8sClient kubernetes.Interface
	k8sConfig, err := rest.InClusterConfig()
	if err != nil {
		log.Printf("Not running in Kubernetes cluster: %v", err)
	} else {
		k8sClient, err = kubernetes.NewForConfig(k8sConfig)
		if err != nil {
			log.Printf("Failed to create kubernetes client: %v", err)
		}
	}

	// Initialize and start cron manager
	cronManager := cron.NewCronManager(
		cfg,
		appLogger,
		k8sClient,
		svcs.DomainService,
		svcs.MailboxService,
		repos,
	)

	if !cfg.AppConfig.CronDisable {
		// If running in Kubernetes, use leader election
		if k8sClient != nil {
			podName := os.Getenv("POD_NAME")
			if podName == "" {
				log.Fatal("POD_NAME environment variable not set")
			}
			namespace := os.Getenv("POD_NAMESPACE")
			if namespace == "" {
				log.Fatal("POD_NAMESPACE environment variable not set")
			}

			go func() {
				if err := cronManager.Start(podName, namespace); err != nil {
					log.Fatalf("Failed to start cron manager: %v", err)
				}
			}()
		} else {
			// Local development - start cron manager directly
			log.Println("Running in local mode - starting cron manager without leader election")
			go func() {
				cronManager.StartCron()
			}()
		}
	}

	return &Server{
		config:       cfg,
		router:       router,
		natsConn:     natsConn,
		cronMgr:      cronManager,
		services:     svcs,
		repositories: repos,
		httpServer: &http.Server{
			Addr:    ":" + cfg.AppConfig.APIPort,
			Handler: router,
		},
		logger: appLogger,
	}, nil
}

func (s *Server) Initialize(ctx context.Context) error {
	// Register webhook handler
	log.Println("Registering event handler...")

	// Setup API routes
	api.RegisterRoutes(ctx, s.router, s.services, s.repositories, s.config, s.logger)

	return nil
}

func (s *Server) recoverWithTelemetry(name string) {
	if r := recover(); r != nil {
		// Get the current span from context or create new one
		tracer := otel.Tracer("github.com/customeros/mailstack")

		// Create a new span as a child of the current trace if it exists
		// We use context.Background() here since this is a goroutine recovery
		// and we want to ensure we capture the panic even if the context is lost
		ctx := context.Background()
		_, span := tracer.Start(ctx, fmt.Sprintf("panic.%s", name))
		defer span.End()

		// Mark span as failed
		span.SetStatus(codes.Error, fmt.Sprintf("panic: %v", r))
		span.RecordError(fmt.Errorf("panic: %v", r))

		// Log panic details
		span.SetAttributes(
			attribute.String("event", "panic"),
			attribute.String("process", name),
			attribute.String("error", fmt.Sprintf("%v", r)),
			attribute.String("stack", string(debug.Stack())),
			attribute.String("time", time.Now().Format(time.RFC3339)),
		)

		// Log stack trace as an event
		span.AddEvent("panic.stack", trace.WithAttributes(
			attribute.String("stack", string(debug.Stack())),
		))

		log.Printf("❌ Panic in %s: %v\n%s", name, r, debug.Stack())
	}
}

func (s *Server) wrapGoroutine(name string, fn func()) {
	defer s.recoverWithTelemetry(name)
	fn()
}

func (s *Server) Run() error {
	// Create root context for the application
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize server components
	if err := s.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize server: %w", err)
	}

	// Starting services
	log.Println("Starting services...")
	if err := s.services.Start(ctx); err != nil {
		return fmt.Errorf("failed to start services: %w", err)
	}
	log.Println("✅ Services started successfully")

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
	defer s.recoverWithTelemetry("shutdown")

	// Set up signal handling for graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Wait for termination signal
	<-stop
	log.Println("Shutting down...")

	// Create a context with timeout for shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("❌ HTTP server shutdown error: %v", err)
	} else {
		log.Println("✅ HTTP server shut down successfully")
	}

	// Stop cron manager when server stops
	s.cronMgr.Stop()
	log.Println("Shutdown complete")

	// Close NATS connection
	if s.natsConn != nil {
		log.Println("Closing NATS connection...")
		s.natsConn.Close()
		log.Println("✅ NATS connection closed")
	}

	// Stop services
	log.Println("Stopping services...")
	if err := s.services.Stop(shutdownCtx); err != nil {
		log.Printf("⚠️ Services shutdown error: %v", err)
	} else {
		log.Println("✅ Services stopped successfully")
	}

	return nil
}
