package repository

import (
	"time"

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
	OrphanEmailRepository     interfaces.OrphanEmailRepository
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
		OrphanEmailRepository:     NewOrphanEmailRepository(mailstackDB),
	}
}

func MigrateDB(dbConfig *config.MailstackDatabaseConfig, mailstackDB *gorm.DB) error {
	db, err := mailstackDB.DB()
	if err != nil {
		return err
	}

	// Temporarily reduce pool size during migration
	db.SetMaxOpenConns(5)

	err = mailstackDB.AutoMigrate(
		&models.Email{},
		&models.EmailAttachment{},
		&models.EmailThread{},
		&models.Mailbox{},
		&models.MailboxSyncState{},
		&models.OrphanEmail{},
	)

	db.Close()

	db, _ = mailstackDB.DB()
	db.SetMaxIdleConns(dbConfig.MaxIdleConn)
	db.SetMaxOpenConns(dbConfig.MaxConn)
	db.SetConnMaxLifetime(time.Duration(dbConfig.ConnMaxLifetime) * time.Minute)

	return err
}
