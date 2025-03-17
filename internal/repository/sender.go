package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/customeros/mailstack/internal/models"
)

// Common repository errors

// SenderRepository defines the interface for sender data operations
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

// GormSenderRepository implements SenderRepository using GORM
type GormSenderRepository struct {
	db *gorm.DB
}

// NewSenderRepository creates a new sender repository instance
func NewSenderRepository(db *gorm.DB) SenderRepository {
	return &GormSenderRepository{db: db}
}

// Create adds a new sender to the database
func (r *GormSenderRepository) Create(ctx context.Context, sender *models.Sender) error {
	if sender == nil {
		return ErrInvalidInput
	}

	// If this is the default sender, unset any existing defaults for this user
	if sender.IsDefault {
		if err := r.unsetDefaultsForUser(sender.UserID); err != nil {
			return err
		}
	}

	sender.CreatedAt = time.Now()
	sender.UpdatedAt = time.Now()

	result := r.db.WithContext(ctx).Create(sender)
	return result.Error
}

// GetByID retrieves a sender by its ID
func (r *GormSenderRepository) GetByID(ctx context.Context, id string) (*models.Sender, error) {
	if id == "" {
		return nil, ErrInvalidInput
	}

	var sender models.Sender
	result := r.db.WithContext(ctx).Where("id = ?", id).First(&sender)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrSenderNotFound
		}
		return nil, result.Error
	}

	return &sender, nil
}

// GetByUserID retrieves all senders for a specific user
func (r *GormSenderRepository) GetByUserID(ctx context.Context, userID string) ([]models.Sender, error) {
	if userID == "" {
		return nil, ErrInvalidInput
	}

	var senders []models.Sender
	result := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("is_default DESC, display_name ASC").Find(&senders)
	if result.Error != nil {
		return nil, result.Error
	}

	return senders, nil
}

// GetDefaultForUser retrieves the default sender for a specific user
func (r *GormSenderRepository) GetDefaultForUser(ctx context.Context, userID string) (*models.Sender, error) {
	if userID == "" {
		return nil, ErrInvalidInput
	}

	var sender models.Sender
	result := r.db.WithContext(ctx).Where("user_id = ? AND is_default = ?", userID, true).First(&sender)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrSenderNotFound
		}
		return nil, result.Error
	}

	return &sender, nil
}

// Update updates an existing sender
func (r *GormSenderRepository) Update(ctx context.Context, sender *models.Sender) error {
	if sender == nil || sender.ID == "" {
		return ErrInvalidInput
	}

	// Check if sender exists
	var exists models.Sender
	result := r.db.WithContext(ctx).Where("id = ?", sender.ID).First(&exists)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return ErrSenderNotFound
		}
		return result.Error
	}

	// If this is being set as default, unset any existing defaults for this user
	if sender.IsDefault && !exists.IsDefault {
		if err := r.unsetDefaultsForUser(sender.UserID); err != nil {
			return err
		}
	}

	sender.UpdatedAt = time.Now()

	// Update only specific fields to avoid overwriting data not included in the update
	updateResult := r.db.WithContext(ctx).Model(&models.Sender{}).
		Where("id = ?", sender.ID).
		Updates(map[string]interface{}{
			"display_name":    sender.DisplayName,
			"signature_html":  sender.SignatureHTML,
			"signature_plain": sender.SignaturePlain,
			"is_default":      sender.IsDefault,
			"is_active":       sender.IsActive,
			"updated_at":      sender.UpdatedAt,
		})

	return updateResult.Error
}

// SetDefault sets a sender as the default for a user
func (r *GormSenderRepository) SetDefault(ctx context.Context, id string, userID string) error {
	if id == "" || userID == "" {
		return ErrInvalidInput
	}

	// Start a transaction
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Unset all default senders for this user
	if err := tx.Model(&models.Sender{}).
		Where("user_id = ?", userID).
		Update("is_default", false).Error; err != nil {
		tx.Rollback()
		return err
	}

	// Set the specified sender as default
	result := tx.Model(&models.Sender{}).
		Where("id = ? AND user_id = ?", id, userID).
		Update("is_default", true)
	if result.Error != nil {
		tx.Rollback()
		return result.Error
	}

	if result.RowsAffected == 0 {
		tx.Rollback()
		return ErrSenderNotFound
	}

	return tx.Commit().Error
}

// SetActive sets the active status of a sender
func (r *GormSenderRepository) SetActive(ctx context.Context, id string, isActive bool) error {
	if id == "" {
		return ErrInvalidInput
	}

	result := r.db.WithContext(ctx).Model(&models.Sender{}).
		Where("id = ?", id).
		Update("is_active", isActive)

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return ErrSenderNotFound
	}

	return nil
}

// Delete removes a sender from the database
func (r *GormSenderRepository) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidInput
	}

	result := r.db.WithContext(ctx).Delete(&models.Sender{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return ErrSenderNotFound
	}

	return nil
}

// ListByTenant retrieves all senders for a specific tenant with pagination
func (r *GormSenderRepository) ListByTenant(ctx context.Context, tenant string, limit, offset int) ([]models.Sender, int64, error) {
	if tenant == "" {
		return nil, 0, ErrInvalidInput
	}

	var senders []models.Sender
	var totalCount int64

	// Get total count
	if err := r.db.WithContext(ctx).Model(&models.Sender{}).
		Where("tenant = ?", tenant).
		Count(&totalCount).Error; err != nil {
		return nil, 0, err
	}

	// Apply pagination
	if limit <= 0 {
		limit = 10 // Default limit
	}

	// Get the data
	result := r.db.WithContext(ctx).
		Where("tenant = ?", tenant).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&senders)

	if result.Error != nil {
		return nil, 0, result.Error
	}

	return senders, totalCount, nil
}

// unsetDefaultsForUser is a helper method to unset all default senders for a user
func (r *GormSenderRepository) unsetDefaultsForUser(userID string) error {
	return r.db.Model(&models.Sender{}).
		Where("user_id = ? AND is_default = ?", userID, true).
		Update("is_default", false).
		Error
}
