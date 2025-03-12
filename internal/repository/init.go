package repository

import (
	"gorm.io/gorm"

	"github.com/customeros/mailstack/config"
	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/services/storage"
)

type Repositories struct {
	EmailRepository           interfaces.EmailRepository
	EmailAttachmentRepository interfaces.EmailAttachmentRepository
	MailboxRepository         interfaces.MailboxRepository
	MailboxSyncRepository     interfaces.MailboxSyncRepository
}

func InitRepositories(mailstackDB *gorm.DB, r2Config *config.R2StorageConfig) *Repositories {
	emailAttachmentStorage := storage.NewR2StorageService(
		r2Config.AccountID,
		r2Config.AccessKeyID,
		r2Config.AccessKeySecret,
		r2Config.EmailAttachmentBucket,
		false, // private access
	)

	return &Repositories{
		EmailRepository:           NewEmailRepository(mailstackDB),
		EmailAttachmentRepository: NewEmailAttachmentRepository(mailstackDB, emailAttachmentStorage),
		MailboxRepository:         NewMailboxRepository(mailstackDB),
		MailboxSyncRepository:     NewMailboxSyncRepository(mailstackDB),
	}
}

func MigrateDB(mailstackDB *gorm.DB) error {
	return mailstackDB.AutoMigrate(
		&models.Email{},
		&models.EmailAttachment{},
		&models.Mailbox{},
		&models.MailboxSyncState{},
	)
}
