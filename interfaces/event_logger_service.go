package interfaces

import (
	"context"

	"github.com/customeros/mailstack/internal/models"
)

type EventLoggerService interface {
	NewEmailEventRecord(ctx context.Context) *models.EmailEvent
	StoreEmailEventInR2(ctx context.Context, eventID string, data []byte) (string, error)
	LogEmailEventToTimescale(ctx context.Context, event *models.EmailEvent)
}
