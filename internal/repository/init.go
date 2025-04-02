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
	DomainRepository          DomainRepository
	EmailRepository           interfaces.EmailRepository
	EmailAttachmentRepository interfaces.EmailAttachmentRepository
	EmailEventRepository      interfaces.EmailEventRepository
	EmailLogRepository        interfaces.EmailLogRepository
	EmailThreadRepository     interfaces.EmailThreadRepository
	MailboxRepository         interfaces.MailboxRepository
	MailboxSyncRepository     interfaces.MailboxSyncRepository
	OrphanEmailRepository     interfaces.OrphanEmailRepository
	SenderRepository          interfaces.SenderRepository
}

func InitRepositories(mailstackDB *gorm.DB, timescaleDB *gorm.DB, r2Config *config.R2StorageConfig) *Repositories {
	emailAttachmentStorage := storage.NewR2StorageService(
		r2Config.AccountID,
		r2Config.AccessKeyID,
		r2Config.AccessKeySecret,
		r2Config.EmailAttachmentBucket,
		false, // private access
	)

	return &Repositories{
		// Mailstack
		DomainRepository:          NewDomainRepository(mailstackDB),
		EmailRepository:           NewEmailRepository(mailstackDB),
		EmailAttachmentRepository: NewEmailAttachmentRepository(mailstackDB, emailAttachmentStorage),
		EmailThreadRepository:     NewEmailThreadRepository(mailstackDB),
		MailboxRepository:         NewMailboxRepository(mailstackDB),
		MailboxSyncRepository:     NewMailboxSyncRepository(mailstackDB),
		OrphanEmailRepository:     NewOrphanEmailRepository(mailstackDB),
		SenderRepository:          NewSenderRepository(mailstackDB),
		// Timescale
		EmailEventRepository: NewEmailEventRepository(timescaleDB),
		EmailLogRepository:   NewEmailLogRepository(timescaleDB),
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
		&models.DMARCMonitoring{},
		&models.MailStackDomain{},
		&models.MailstackReputation{},
	)

	db.SetMaxIdleConns(dbConfig.MaxIdleConn)
	db.SetMaxOpenConns(dbConfig.MaxConn)
	db.SetConnMaxLifetime(time.Duration(dbConfig.ConnMaxLifetime) * time.Minute)

	return err
}

func MigrateTimescaleDB(dbConfig *config.TimescaleDBConfig, timescaleDB *gorm.DB) error {
	db, err := timescaleDB.DB()
	if err != nil {
		return err
	}

	db.SetMaxOpenConns(5)

	err = timescaleDB.AutoMigrate(
		&models.EmailEvent{},
		&models.EmailLog{},
	)

	db.SetMaxIdleConns(dbConfig.MaxIdleConn)
	db.SetMaxOpenConns(dbConfig.MaxConn)
	db.SetConnMaxLifetime(time.Duration(dbConfig.ConnMaxLifetime) * time.Minute)

	return err
}
