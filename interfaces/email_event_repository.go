package interfaces

import (
	"context"

	"github.com/customeros/mailstack/internal/models"
)

type EmailEvent interface {
	Create(ctx context.Context, emailEvent *models.EmailEvent) error
	IsDuplicateByHash(ctx context.Context, hash string) (bool, error)
}
