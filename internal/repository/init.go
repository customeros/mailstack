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
	EmailThreadRepository     interfaces.EmailThreadRepository
	MailboxRepository         interfaces.MailboxRepository
	MailboxSyncRepository     interfaces.MailboxSyncRepository
	DomainRepository          DomainRepository
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
		EmailThreadRepository:     NewEmailThreadRepository(mailstackDB),
		MailboxRepository:         NewMailboxRepository(mailstackDB),
		MailboxSyncRepository:     NewMailboxSyncRepository(mailstackDB),
		DomainRepository:          NewDomainRepository(mailstackDB),
	}
}

func MigrateDB(mailstackDB *gorm.DB) error {
	return mailstackDB.AutoMigrate(
		&models.Domain{},
		&models.DMARCMonitoring{},
		&models.Email{},
		&models.EmailAttachment{},
		&models.EmailThread{},
		&models.Mailbox{},
		&models.MailboxSyncState{},
		&models.MailstackReputation{},
	)
}
