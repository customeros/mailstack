package interfaces

import (
	"context"

	"github.com/customeros/mailstack/internal/models"
)

type EmailEventRepository interface {
	Create(ctx context.Context, emailEvent *models.EmailEvent) error
}
