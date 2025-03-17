package models

import (
	"time"

	"gorm.io/gorm"

	"github.com/customeros/mailstack/internal/utils"
)

type Sender struct {
	ID             string    `gorm:"column:id;type:varchar(50);primaryKey" json:"id"`
	Tenant         string    `gorm:"column:tenant;type:varchar(255)" json:"tenant"`
	UserID         string    `gorm:"column:user_id;type:varchar(255);index" json:"userId"`
	DisplayName    string    `gorm:"column:display_name;type:varchar(255)" json:"displayName"`
	SignatureHTML  string    `gorm:"column:signature_html;type:text" json:"signatureHtml"`
	SignaturePlain string    `gorm:"column:signature_plain;type:text" json:"signaturePlain"`
	IsDefault      bool      `gorm:"column:is_default;type:boolean" json:"isDefault"`
	IsActive       bool      `gorm:"column:is_active;type:boolean" json:"isActive"`
	CreatedAt      time.Time `gorm:"column:created_at;type:timestamp;default:current_timestamp" json:"createdAt"`
	UpdatedAt      time.Time `gorm:"column:updated_at;type:timestamp;default:current_timestamp" json:"updatedAt"`
}

func (Sender) TableName() string {
	return "senders"
}

func (m *Sender) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = utils.GenerateNanoIDWithPrefix("sndr", 16)
	}
	return nil
}
