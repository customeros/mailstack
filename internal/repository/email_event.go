package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/telemetry"
)

type emailEventRepository struct {
	db *gorm.DB
}

func NewEmailEventRepository(db *gorm.DB) interfaces.EmailEvent {
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

func (r *emailEventRepository) IsDuplicateByHash(ctx context.Context, hash string) (bool, error) {
	spans, ctx := telemetry.StartPostgresSpan(ctx, "emailEventRepository.IsDuplicateByHash")
	defer spans.Finish()

	var count int64
	result := r.db.WithContext(ctx).
		Model(&models.EmailEvent{}).
		Where("hash = ?", hash).
		Count(&count)

	if result.Error != nil {
		err := fmt.Errorf("error checking for duplicate email: %w", result.Error)
		spans.TraceError(err)
		return false, err
	}

	return count > 0, nil
}
