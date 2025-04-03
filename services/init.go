package services

import (
	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/config"
	"github.com/customeros/mailstack/internal/logger"
	nats_internal "github.com/customeros/mailstack/internal/nats"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/services/cloudflare"
	"github.com/customeros/mailstack/services/domain"
	"github.com/customeros/mailstack/services/email"
	email_analysis "github.com/customeros/mailstack/services/email_anaylsis"
	"github.com/customeros/mailstack/services/email_classification"
	"github.com/customeros/mailstack/services/email_content"
	"github.com/customeros/mailstack/services/email_storage"
	"github.com/customeros/mailstack/services/event_logger"
	"github.com/customeros/mailstack/services/imap"
	"github.com/customeros/mailstack/services/mailbox"
	mailboxold "github.com/customeros/mailstack/services/mailbox_old"
	"github.com/customeros/mailstack/services/namecheap"
	"github.com/customeros/mailstack/services/opensrs"
	"github.com/customeros/mailstack/services/storage"
)

type Services struct {
	CloudflareService          interfaces.CloudflareService
	EmailService               interfaces.EmailService
	EmailAnaylsisService       interfaces.EmailProcessor
	EmailClassificationService interfaces.EmailProcessor
	EmailContentService        interfaces.EmailProcessor
	EmailStorageService        interfaces.EmailProcessor
	EventLoggerService         *event_logger.EventLoggerService
	IMAPService                interfaces.IMAPService
	MailboxService             interfaces.MailboxService
	NamecheapService           interfaces.NamecheapService
	OpenSrsService             interfaces.OpenSrsService

	MailboxServiceOld interfaces.MailboxServiceOld
	DomainService     interfaces.DomainService
}

func InitServices(natsConn *nats_internal.NATSConnections, log logger.Logger, repos *repository.Repositories, cfg *config.Config) (*Services, error) {
	emlStorage := storage.NewR2StorageService(
		cfg.R2StorageConfig.AccountID,
		cfg.R2StorageConfig.AccessKeyID,
		cfg.R2StorageConfig.AccessKeySecret,
		cfg.AppConfig.EMLStorageBucket,
		false,
	)

	eventsStorage := storage.NewR2StorageService(
		cfg.R2StorageConfig.AccountID,
		cfg.R2StorageConfig.AccessKeyID,
		cfg.R2StorageConfig.AccessKeySecret,
		cfg.AppConfig.EventsStorageBucket,
		false,
	)

	namecheapImpl := namecheap.NewNamecheapService(cfg.NamecheapConfig, repos)
	cloudflareImpl := cloudflare.NewCloudflareService(log, cfg.CloudflareConfig, repos)
	opensrsImpl := opensrs.NewOpenSRSService(log, cfg.OpenSrsConfig, repos)
	mailboxOldImpl := mailboxold.NewMailboxServiceOld(log, repos, opensrsImpl)
	imapImpl := imap.NewIMAPService(natsConn, repos)
	eventLogger := event_logger.NewEventLoggerService(repos, eventsStorage)

	services := Services{
		CloudflareService: cloudflareImpl,
		EmailService:      email.NewEmailService(repos),
		EmailAnaylsisService: email_analysis.NewEmailAnalysisService(
			cfg.CustomerOSAPIConfig,
			natsConn,
			repos,
			eventLogger,
		),
		EmailClassificationService: email_classification.NewEmailClassificationService(natsConn, repos, eventLogger),
		EmailContentService:        email_content.NewEmailContentService(natsConn, repos, eventLogger, emlStorage),
		EmailStorageService:        email_storage.NewEmailStorageService(natsConn, repos, eventLogger, imapImpl, emlStorage),
		IMAPService:                imapImpl,
		MailboxService:             mailbox.NewMailboxService(repos, imapImpl, opensrsImpl),
		NamecheapService:           namecheapImpl,
		OpenSrsService:             opensrsImpl,

		MailboxServiceOld: mailboxOldImpl,
		DomainService:     domain.NewDomainService(repos, cloudflareImpl, namecheapImpl, mailboxOldImpl, opensrsImpl),
	}

	return &services, nil
}
