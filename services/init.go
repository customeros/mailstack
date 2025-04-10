package services

import (
	"context"
	"fmt"

	"go.uber.org/multierr"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/config"
	"github.com/customeros/mailstack/internal/logger"
	nats_internal "github.com/customeros/mailstack/internal/nats"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/services/cloudflare"
	"github.com/customeros/mailstack/services/domain"
	"github.com/customeros/mailstack/services/email"
	email_analysis "github.com/customeros/mailstack/services/email_anaylsis"
	"github.com/customeros/mailstack/services/email_attachment"
	"github.com/customeros/mailstack/services/email_classification"
	"github.com/customeros/mailstack/services/email_content"
	"github.com/customeros/mailstack/services/email_storage"
	"github.com/customeros/mailstack/services/email_thread"
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
	EmailAttachmentService     interfaces.EmailProcessor
	EmailClassificationService interfaces.EmailProcessor
	EmailContentService        interfaces.EmailProcessor
	EmailStorageService        interfaces.EmailProcessor
	EmailThreadingService      interfaces.EmailProcessor
	EventLoggerService         interfaces.EmailProcessor
	IMAPService                interfaces.IMAPService
	MailboxService             interfaces.MailboxService
	NamecheapService           interfaces.NamecheapService
	OpenSrsService             interfaces.OpenSrsService

	MailboxServiceOld interfaces.MailboxServiceOld
	DomainService     interfaces.DomainService
}

func InitServices(natsConn *nats_internal.NATSConnections, log logger.Logger, repos *repository.Repositories, cfg *config.Config) *Services {
	emlStorage := storage.NewR2StorageService(
		cfg.R2StorageConfig.AccountID,
		cfg.R2StorageConfig.AccessKeyID,
		cfg.R2StorageConfig.AccessKeySecret,
		cfg.AppConfig.EMLStorageBucket,
		false,
	)

	namecheapImpl := namecheap.NewNamecheapService(cfg.NamecheapConfig, repos)
	cloudflareImpl := cloudflare.NewCloudflareService(log, cfg.CloudflareConfig, repos)
	opensrsImpl := opensrs.NewOpenSRSService(log, cfg.OpenSrsConfig, repos)
	mailboxOldImpl := mailboxold.NewMailboxServiceOld(log, repos, opensrsImpl)
	imapImpl := imap.NewIMAPService(natsConn, repos)

	services := Services{
		CloudflareService: cloudflareImpl,
		EmailService:      email.NewEmailService(repos),
		EmailAnaylsisService: email_analysis.NewEmailAnalysisService(
			cfg.CustomerOSAPIConfig,
			natsConn,
			repos,
		),
		EmailAttachmentService:     email_attachment.NewEmailAttachmentService(natsConn, repos),
		EmailClassificationService: email_classification.NewEmailClassificationService(natsConn, repos),
		EmailContentService:        email_content.NewEmailContentService(natsConn, repos, emlStorage),
		EmailStorageService:        email_storage.NewEmailStorageService(natsConn, repos, imapImpl, emlStorage),
		EmailThreadingService:      email_thread.NewEmailThreadingService(natsConn, repos),
		EventLoggerService:         event_logger.NewEventLoggerService(natsConn, repos),
		IMAPService:                imapImpl,
		MailboxService:             mailbox.NewMailboxService(repos, imapImpl, opensrsImpl),
		NamecheapService:           namecheapImpl,
		OpenSrsService:             opensrsImpl,

		MailboxServiceOld: mailboxOldImpl,
		DomainService:     domain.NewDomainService(repos, cloudflareImpl, namecheapImpl, mailboxOldImpl, opensrsImpl),
	}

	return &services
}

// Improved Start method with better error handling
func (s *Services) Start(ctx context.Context) error {
	services := []struct {
		name    string
		starter func(context.Context) error
	}{
		{"IMAP", s.IMAPService.Start},
		{"Email Analysis", s.EmailAnaylsisService.Start},
		{"Email Attachment", s.EmailAttachmentService.Start},
		{"Email Classification", s.EmailClassificationService.Start},
		{"Email Content", s.EmailContentService.Start},
		{"Email Storage", s.EmailStorageService.Start},
		{"Email Threading", s.EmailThreadingService.Start},
		{"Event Logger", s.EventLoggerService.Start},
	}

	for _, svc := range services {
		if err := svc.starter(ctx); err != nil {
			return fmt.Errorf("failed to start %s service: %w", svc.name, err)
		}
	}

	return nil
}

func (s *Services) Stop(ctx context.Context) error {
	services := []struct {
		name    string
		stopper func(context.Context) error
	}{
		{"IMAP", func(ctx context.Context) error { return s.IMAPService.Stop() }},
		{"Email Analysis", func(ctx context.Context) error { return s.EmailAnaylsisService.Close() }},
		{"Email Attachment", func(ctx context.Context) error { return s.EmailAttachmentService.Close() }},
		{"Email Classification", func(ctx context.Context) error { return s.EmailClassificationService.Close() }},
		{"Email Content", func(ctx context.Context) error { return s.EmailContentService.Close() }},
		{"Email Storage", func(ctx context.Context) error { return s.EmailStorageService.Close() }},
		{"Email Threading", func(ctx context.Context) error { return s.EmailThreadingService.Close() }},
		{"Event Logger", func(ctx context.Context) error { return s.EventLoggerService.Close() }},
	}

	var errs error
	for _, svc := range services {
		if err := svc.stopper(ctx); err != nil {
			errs = multierr.Append(errs, fmt.Errorf("failed to stop %s service: %w", svc.name, err))
		}
	}

	return errs
}
