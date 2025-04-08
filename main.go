package main

import (
	"fmt"
	"log"
	"os"

	"github.com/customeros/mailstack/internal/config"
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
	})
	if err != nil {
		log.Fatalf("Mailstack database initialization failed: %v", err)
	}

	warehouseDB, err := database.InitDataWarehouse(&database.DatabaseConfig{
		DBName:          cfg.DataWarehouseConfig.DBName,
		Host:            cfg.DataWarehouseConfig.Host,
		Port:            cfg.DataWarehouseConfig.Port,
		User:            cfg.DataWarehouseConfig.User,
		Password:        cfg.DataWarehouseConfig.Password,
		MaxConn:         cfg.DataWarehouseConfig.MaxConn,
		MaxIdleConn:     cfg.DataWarehouseConfig.MaxIdleConn,
		ConnMaxLifetime: cfg.DataWarehouseConfig.ConnMaxLifetime,
		LogLevel:        cfg.DataWarehouseConfig.LogLevel,
	})
	if err != nil {
		log.Fatalf("Openline database initialization failed: %v", err)
	}

	switch os.Args[1] {
	case "migrate":
		// Run Mailstack database migrations
		err := repository.MigrateMailstackDB(cfg.MailstackDatabaseConfig, mailstackDB)
		if err != nil {
			log.Fatalf("Mailstack database migration failed: %v", err)
		}
		log.Println("Mailstack database migration completed successfully")

		// Run DataWarehouse migrations
		err = repository.MigrateDataWarehouse(cfg.DataWarehouseConfig, warehouseDB)
		if err != nil {
			log.Fatalf("Warehouse database migration failed: %v", err)
		}
		log.Println("Warehouse database migration completed successfully")

	case "server":
		log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
		log.Println("MailStack starting up...")

		srv, err := server.NewServer(cfg, mailstackDB, warehouseDB)
		if err != nil {
			log.Fatalf("Server setup failed: %v", err)
		}

		// Start the server
		err = srv.Run()
		if err != nil {
			log.Fatalf("Server startup failed: %v", err)
		}

	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		fmt.Println("Usage: mailstack <command>")
		fmt.Println("Commands:")
		fmt.Println("  migrate   Run database migrations")
		fmt.Println("  server    Start the application server")
		os.Exit(1)
	}
}
