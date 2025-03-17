package interfaces

import (
	"context"

	"github.com/customeros/mailstack/internal/models"
)

type SenderRepository interface {
	Create(ctx context.Context, sender *models.Sender) error
	GetByID(ctx context.Context, id string) (*models.Sender, error)
	GetByUserID(ctx context.Context, userID string) ([]models.Sender, error)
	GetDefaultForUser(ctx context.Context, userID string) (*models.Sender, error)
	Update(ctx context.Context, sender *models.Sender) error
	SetDefault(ctx context.Context, id string, userID string) error
	SetActive(ctx context.Context, id string, isActive bool) error
	Delete(ctx context.Context, id string) error
	ListByTenant(ctx context.Context, tenant string, limit, offset int) ([]models.Sender, int64, error)
}
