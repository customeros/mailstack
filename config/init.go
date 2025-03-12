package config

import (
	"log"

	"github.com/caarlos0/env/v6"
	"github.com/joho/godotenv"

	"github.com/customeros/mailstack/internal/logger"
	"github.com/customeros/mailstack/internal/tracing"
)

type Config struct {
	AppConfig               *AppConfig
	Logger                  *logger.Config
	Tracing                 *tracing.JaegerConfig
	MailstackDatabaseConfig *MailstackDatabaseConfig
	OpenlineDatabaseConfig  *OpenlineDatabaseConfig
	R2StorageConfig         *R2StorageConfig
}

func InitConfig() (*Config, error) {
	config := &Config{
		AppConfig:               &AppConfig{},
		Logger:                  &logger.Config{},
		Tracing:                 &tracing.JaegerConfig{},
		MailstackDatabaseConfig: &MailstackDatabaseConfig{},
		OpenlineDatabaseConfig:  &OpenlineDatabaseConfig{},
		R2StorageConfig:         &R2StorageConfig{},
	}

	err := godotenv.Load()
	if err != nil {
		log.Print("Unable to load .env file")
	}

	err = env.Parse(config)
	if err != nil {
		log.Fatalf("Error loading mailstack config: %v", err)
	}

	return config, nil
}
