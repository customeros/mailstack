package database

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/customeros/mailstack/internal/models"
	"github.com/uptrace/go-clickhouse/ch"
)

type ClickhouseConfig struct {
	Host              string
	Port              uint16
	User              string
	Password          string
	MailstackDatabase string
	LogsDatabase      string
}

var (
	mainDB *ch.DB
	logsDB *ch.DB
)

// InitClickhouseDatabases initializes connections to both main and logs databases
func InitClickhouseDatabases(config ClickhouseConfig) error {
	// Initialize main database connection
	mainConn := ch.Connect(
		ch.WithAddr(fmt.Sprintf("%s:%d", config.Host, config.Port)),
		ch.WithUser(config.User),
		ch.WithPassword(config.Password),
		ch.WithDatabase(config.MailstackDatabase),
		ch.WithAutoCreateDatabase(true),
		ch.WithPoolSize(20),
		ch.WithConnMaxIdleTime(time.Hour),
		ch.WithTimeout(30*time.Second),
		ch.WithDialTimeout(10*time.Second),
		ch.WithReadTimeout(30*time.Second),
		ch.WithWriteTimeout(30*time.Second),
		ch.WithQuerySettings(map[string]interface{}{
			"max_execution_time":            60,
			"send_progress_in_http_headers": 0,
			// S3-specific settings
			"use_nulls":                     1,
			"allow_experimental_s3":         1,
			"allow_experimental_merge_tree": 1,
		}),
	)

	// Verify main connection
	if err := mainConn.Ping(context.Background()); err != nil {
		return fmt.Errorf("main clickhouse connection failed: %w", err)
	}
	mainDB = mainConn

	// Initialize logs database connection
	logsConn := ch.Connect(
		ch.WithAddr(fmt.Sprintf("%s:%d", config.Host, config.Port)),
		ch.WithUser(config.User),
		ch.WithPassword(config.Password),
		ch.WithDatabase(config.LogsDatabase),
		ch.WithAutoCreateDatabase(true),
		ch.WithPoolSize(20),
		ch.WithConnMaxIdleTime(time.Hour),
		ch.WithTimeout(30*time.Second),
		ch.WithDialTimeout(10*time.Second),
		ch.WithReadTimeout(30*time.Second),
		ch.WithWriteTimeout(30*time.Second),
		ch.WithQuerySettings(map[string]interface{}{
			"max_execution_time":            60,
			"send_progress_in_http_headers": 0,
			// S3-specific settings
			"use_nulls":                     1,
			"allow_experimental_s3":         1,
			"allow_experimental_merge_tree": 1,
		}),
	)

	// Verify logs connection
	if err := logsConn.Ping(context.Background()); err != nil {
		return fmt.Errorf("logs clickhouse connection failed: %w", err)
	}
	logsDB = logsConn

	return nil
}

// MigrateClickhouseDatabases runs migrations for both databases
func MigrateClickhouseDatabases() error {
	// Create logs database if it doesn't exist
	if err := createDatabaseIfNotExists(mainDB, "logs"); err != nil {
		return fmt.Errorf("failed to create logs database: %w", err)
	}

	// Migrate tables
	if err := migrateMainDB(); err != nil {
		return fmt.Errorf("failed to migrate main database: %w", err)
	}

	if err := migrateLogsDB(); err != nil {
		return fmt.Errorf("failed to migrate logs database: %w", err)
	}

	return nil
}

// createDatabaseIfNotExists creates a database if it doesn't exist
func createDatabaseIfNotExists(db *ch.DB, dbName string) error {
	query := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", dbName)
	_, err := db.Exec(query)
	return err
}

// migrateMainDB migrates tables in the main database
func migrateMainDB() error {
	// Migrate EmailStore table
	if err := migrateTable(mainDB, &models.EmailStore{}); err != nil {
		return fmt.Errorf("failed to migrate EmailStore table: %w", err)
	}
	return nil
}

// migrateLogsDB migrates tables in the logs database
func migrateLogsDB() error {
	// Migrate LogEntry table
	if err := migrateTable(logsDB, &models.LogEntry{}); err != nil {
		return fmt.Errorf("failed to migrate LogEntry table: %w", err)
	}

	// Migrate TraceEntry table
	if err := migrateTable(logsDB, &models.TraceEntry{}); err != nil {
		return fmt.Errorf("failed to migrate TraceEntry table: %w", err)
	}

	return nil
}

// migrateTable creates a table if it doesn't exist
func migrateTable(db *ch.DB, model interface{}) error {
	if tableCreator, ok := model.(interface{ CreateTableSQL() string }); ok {
		query := tableCreator.CreateTableSQL()
		_, err := db.Exec(query)
		if err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
		log.Printf("Successfully migrated table for %T", model)
		return nil
	}
	return fmt.Errorf("model does not implement CreateTableSQL method")
}

// GetDB returns the main database connection
func GetDB() *ch.DB {
	return mainDB
}

// GetLogsDB returns the logs database connection
func GetLogsDB() *ch.DB {
	return logsDB
}
