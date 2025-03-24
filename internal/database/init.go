package database

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/uptrace/go-clickhouse/ch"
	"gorm.io/gorm"

	"github.com/customeros/mailstack/internal/config"
)

func InitMailstackDatabase(dbConfig *DatabaseConfig) (*gorm.DB, error) {
	db, err := NewConnection(dbConfig)
	if err != nil {
		log.Fatalf("Failed to connect to the database: %v", err)
	}

	return db, nil
}

func InitOpenlineDatabase(dbConfig *DatabaseConfig) (*gorm.DB, error) {
	db, err := NewConnection(dbConfig)
	if err != nil {
		log.Fatalf("Failed to connect to the database: %v", err)
	}

	return db, nil
}

func InitClickhouse(clickhouseConfig *config.ClickhouseConfig) (*ch.DB, error) {
	db := ch.Connect(
		ch.WithAddr(clickhouseConfig.Host+":"+clickhouseConfig.Port),
		ch.WithUser(clickhouseConfig.User),
		ch.WithPassword(clickhouseConfig.Password),
		ch.WithDatabase(clickhouseConfig.DBName),
		ch.WithAutoCreateDatabase(true),
		ch.WithPoolSize(20),
		ch.WithConnMaxIdleTime(time.Hour),
		ch.WithTimeout(30*time.Second),
		ch.WithDialTimeout(10*time.Second),
		ch.WithReadTimeout(30*time.Second),
		ch.WithWriteTimeout(30*time.Second),

		// Query settings
		ch.WithQuerySettings(map[string]interface{}{
			"max_execution_time":            60,
			"send_progress_in_http_headers": 0,
		}),
	)

	// Verify connection
	if err := db.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("clickhouse connection failed: %w", err)
	}

	return db, nil
}
