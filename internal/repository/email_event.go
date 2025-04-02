package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/models"
)

type emailEventRepository struct {
	db *gorm.DB
}

func NewEmailEventRepository(db *gorm.DB) interfaces.EmailEventRepository {
	return &emailEventRepository{
		db: db,
	}
}

func (r *emailEventRepository) Create(ctx context.Context, emailEvent *models.EmailEvent) error {
	result := r.db.WithContext(ctx).Create(emailEvent)
	if result.Error != nil {
		return fmt.Errorf("failed to create email event: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("no rows affected when creating email event")
	}

	return nil
}
