package main

import (
	"fmt"
	"log"
	"os"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/customeros/mailstack/internal/config"
	"github.com/customeros/mailstack/internal/cron"
	"github.com/customeros/mailstack/internal/database"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/server"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: mailstack <command>")
		fmt.Println("Commands:")
		fmt.Println("  migrate   Run database migrations")
		fmt.Println("  server    Start the application server")
		os.Exit(1)
	}

	cfg, err := config.InitConfig()
	if err != nil {
		log.Fatalf("Config initialization failed: %v", err)
	}
	if cfg == nil {
		log.Fatalf("config is empty")
	}

	// Setup the databases
	openlineDB, err := database.InitOpenlineDatabase(&database.DatabaseConfig{
		DBName:          cfg.OpenlineDatabaseConfig.DBName,
		Host:            cfg.OpenlineDatabaseConfig.Host,
		Port:            cfg.OpenlineDatabaseConfig.Port,
		User:            cfg.OpenlineDatabaseConfig.User,
		Password:        cfg.OpenlineDatabaseConfig.Password,
		MaxConn:         cfg.OpenlineDatabaseConfig.MaxConn,
		MaxIdleConn:     cfg.OpenlineDatabaseConfig.MaxIdleConn,
		ConnMaxLifetime: cfg.OpenlineDatabaseConfig.ConnMaxLifetime,
		LogLevel:        cfg.OpenlineDatabaseConfig.LogLevel,
		SSLMode:         cfg.OpenlineDatabaseConfig.SSLMode,
	})
	if err != nil {
		log.Fatalf("Openline database initialization failed: %v", err)
	}

	mailstackDB, err := database.InitMailstackDatabase(&database.DatabaseConfig{
		DBName:          cfg.MailstackDatabaseConfig.DBName,
		Host:            cfg.MailstackDatabaseConfig.Host,
		Port:            cfg.MailstackDatabaseConfig.Port,
		User:            cfg.MailstackDatabaseConfig.User,
		Password:        cfg.MailstackDatabaseConfig.Password,
		MaxConn:         cfg.MailstackDatabaseConfig.MaxConn,
		MaxIdleConn:     cfg.MailstackDatabaseConfig.MaxIdleConn,
		ConnMaxLifetime: cfg.MailstackDatabaseConfig.ConnMaxLifetime,
		LogLevel:        cfg.MailstackDatabaseConfig.LogLevel,
		SSLMode:         cfg.MailstackDatabaseConfig.SSLMode,
	})
	if err != nil {
		log.Fatalf("Mailstack database initialization failed: %v", err)
	}

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

	switch os.Args[1] {
	case "migrate":
		err := repository.MigrateMailstackDB(cfg.MailstackDatabaseConfig, mailstackDB)
		if err != nil {
			log.Fatalf("Mailstack database migration failed: %v", err)
		}
		err = repository.MigrateOpenlineDB(cfg.OpenlineDatabaseConfig, openlineDB)
		if err != nil {
			log.Fatalf("Openline database migration failed: %v", err)
		}
		log.Println("Database migration completed successfully")

	case "server":
		log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
		log.Println("MailStack starting up...")

		srv, err := server.NewServer(cfg, mailstackDB, openlineDB)
		if err != nil {
			log.Fatalf("Server setup failed: %v", err)
		}

		// Initialize and start cron manager
		cronManager := cron.NewCronManager(
			cfg,
			srv.Logger(),
			k8sClient,
			srv.Services().DomainService,
			srv.Services().MailboxService,
		)

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

		// Start the server
		err = srv.Run()
		if err != nil {
			log.Fatalf("Server startup failed: %v", err)
		}

		// Stop cron manager when server stops
		cronManager.Stop()
		log.Println("Shutdown complete")

	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		fmt.Println("Usage: mailstack <command>")
		fmt.Println("Commands:")
		fmt.Println("  migrate   Run database migrations")
		fmt.Println("  server    Start the application server")
		os.Exit(1)
	}
}
