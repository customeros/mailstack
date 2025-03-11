package interfaces

import (
	"context"

	"github.com/customeros/mailstack/internal/models"
)

type EmailFilterService interface {
	ScanEmail(ctx context.Context, email *models.Email) error
}
