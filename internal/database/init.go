package database

import (
	"log"

	"gorm.io/gorm"
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
