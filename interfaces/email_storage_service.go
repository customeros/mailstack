package interfaces

import (
	"context"

	"github.com/customeros/mailstack/internal/models"
)

type EmailStorageService interface {
	Start(ctx context.Context) error
	Close() error
	NewEmailLog() *models.EmailLog
}
