package interfaces

import (
	"context"

	"github.com/customeros/mailstack/internal/models"
)

type EmailLogRepository interface {
	Create(ctx context.Context, emailLog *models.EmailLog) error
	IsDuplicateByHash(ctx context.Context, emailHash string) (bool, error)
}
