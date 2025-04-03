package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

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

func (r *emailLogRepository) UpdateEmailLog(ctx context.Context, id string, updates map[string]interface{}) error {
	if id == "" {
		return errors.New("id is required for update")
	}
	if len(updates) == 0 {
		return errors.New("no update fields provided")
	}

	// Always update the UpdatedAt field
	updates["updated_at"] = time.Now()

	// Use a transaction to ensure consistency
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Update only the specific email log with the given ID
	// The map contains only the fields we want to update
	result := tx.Model(&models.EmailLog{}).
		Where("id = ?", id).
		Updates(updates)

	if result.Error != nil {
		tx.Rollback()
		return result.Error
	}

	if result.RowsAffected == 0 {
		tx.Rollback()
		return fmt.Errorf("email log with ID %s not found", id)
	}

	return tx.Commit().Error
}
