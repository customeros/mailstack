package repository

import (
	"time"

	"gorm.io/gorm"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/config"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/services/storage"
)

type Repositories struct {
	DomainRepository                DomainRepository
	EmailRepository                 interfaces.EmailRepository
	EmailAttachmentRepository       interfaces.EmailAttachmentRepository
	EmailThreadRepository           interfaces.EmailThreadRepository
	MailboxRepository               interfaces.MailboxRepository
	MailboxSyncRepository           interfaces.MailboxSyncRepository
	OrphanEmailRepository           interfaces.OrphanEmailRepository
	SenderRepository                interfaces.SenderRepository
	TenantSettingsMailboxRepository TenantSettingsMailboxRepository
}

func InitRepositories(mailstackDB *gorm.DB, openlineDB *gorm.DB, r2Config *config.R2StorageConfig) *Repositories {
	emailAttachmentStorage := storage.NewR2StorageService(
		r2Config.AccountID,
		r2Config.AccessKeyID,
		r2Config.AccessKeySecret,
		r2Config.EmailAttachmentBucket,
		false, // private access
	)

	return &Repositories{
		// Openline
		DomainRepository:                NewDomainRepository(openlineDB),
		TenantSettingsMailboxRepository: NewTenantSettingsMailboxRepository(openlineDB),
		// Mailstack
		EmailRepository:           NewEmailRepository(mailstackDB),
		EmailAttachmentRepository: NewEmailAttachmentRepository(mailstackDB, emailAttachmentStorage),
		EmailThreadRepository:     NewEmailThreadRepository(mailstackDB),
		MailboxRepository:         NewMailboxRepository(mailstackDB),
		MailboxSyncRepository:     NewMailboxSyncRepository(mailstackDB),
		OrphanEmailRepository:     NewOrphanEmailRepository(mailstackDB),
		SenderRepository:          NewSenderRepository(mailstackDB),
	}
}

func MigrateMailstackDB(dbConfig *config.MailstackDatabaseConfig, mailstackDB *gorm.DB) error {
	db, err := mailstackDB.DB()
	if err != nil {
		return err
	}

	db.SetMaxOpenConns(5)

	err = mailstackDB.AutoMigrate(
		&models.Email{},
		&models.EmailAttachment{},
		&models.EmailThread{},
		&models.Mailbox{},
		&models.MailboxSyncState{},
		&models.OrphanEmail{},
		&models.Sender{},
	)

	db.Close()

	db, _ = mailstackDB.DB()
	db.SetMaxIdleConns(dbConfig.MaxIdleConn)
	db.SetMaxOpenConns(dbConfig.MaxConn)
	db.SetConnMaxLifetime(time.Duration(dbConfig.ConnMaxLifetime) * time.Minute)

	return err
}

func MigrateOpenlineDB(dbConfig *config.OpenlineDatabaseConfig, openlineDB *gorm.DB) error {
	db, err := openlineDB.DB()
	if err != nil {
		return err
	}

	db.SetMaxOpenConns(5)

	err = openlineDB.AutoMigrate(
		&models.DMARCMonitoring{},
		&models.MailStackDomain{},
		&models.TenantSettingsMailbox{},
		&models.MailstackReputation{},
	)

	db.Close()

	db, _ = openlineDB.DB()
	db.SetMaxIdleConns(dbConfig.MaxIdleConn)
	db.SetMaxOpenConns(dbConfig.MaxConn)
	db.SetConnMaxLifetime(time.Duration(dbConfig.ConnMaxLifetime) * time.Minute)

	return err
}
