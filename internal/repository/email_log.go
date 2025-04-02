package repository

import (
	"context"

	"gorm.io/gorm"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/models"
)

type emailLogRepository struct {
	db *gorm.DB
}

func NewEmailLogRepository(db *gorm.DB) interfaces.EmailLogRepository {
	return &emailLogRepository{
		db: db,
	}
}

// Create inserts a new email log record into the database
func (r *emailLogRepository) Create(ctx context.Context, emailLog *models.EmailLog) error {
	return r.db.WithContext(ctx).Create(emailLog).Error
}

// IsDuplicateByHash checks if an email with the given hash already exists
func (r *emailLogRepository) IsDuplicateByHash(ctx context.Context, emailHash string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.EmailLog{}).
		Where("email_hash = ?", emailHash).
		Count(&count).
		Error
	if err != nil {
		return false, err
	}

	return count > 0, nil
}
