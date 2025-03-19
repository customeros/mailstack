package services

import (
	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/config"
	"github.com/customeros/mailstack/internal/logger"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/services/ai"
	"github.com/customeros/mailstack/services/cloudflare"
	"github.com/customeros/mailstack/services/domain"
	"github.com/customeros/mailstack/services/email_filter"
	"github.com/customeros/mailstack/services/events"
	"github.com/customeros/mailstack/services/imap"
	"github.com/customeros/mailstack/services/mailbox"
	mailboxold "github.com/customeros/mailstack/services/mailbox_old"
	"github.com/customeros/mailstack/services/namecheap"
	"github.com/customeros/mailstack/services/opensrs"
)

type Services struct {
	EventsService      *events.EventsService
	AIService          interfaces.AIService
	CloudflareService  interfaces.CloudflareService
	EmailFilterService interfaces.EmailFilterService
	IMAPService        interfaces.IMAPService
	MailboxService     interfaces.MailboxService
	NamecheapService   interfaces.NamecheapService
	OpenSrsService     interfaces.OpenSrsService

	MailboxServiceOld interfaces.MailboxServiceOld
	DomainService     interfaces.DomainService
}

func InitServices(rabbitmqURL string, log logger.Logger, repos *repository.Repositories, cfg *config.Config) (*Services, error) {
	// events
	publisherConfig := &events.PublisherConfig{
		MessageTTL:          events.DefaultMessageTTL,
		MaxRetries:          events.DefaultMaxRetries,
		PublishTimeout:      events.DefaultPublishTimeout,
		ReconnectBackoff:    events.DefaultReconnectBackoff,
		MaxReconnectBackoff: events.DefaultMaxReconnectBackoff,
	}

	events, err := events.NewEventsService(rabbitmqURL, log, publisherConfig)
	if err != nil {
		return nil, err
	}

	aiServiceImpl := ai.NewAIService(cfg.CustomerOSAPIConfig)
	namecheapImpl := namecheap.NewNamecheapService(cfg.NamecheapConfig, repos)
	cloudflareImpl := cloudflare.NewCloudflareService(log, cfg.CloudflareConfig, repos)
	opensrsImpl := opensrs.NewOpenSRSService(log, cfg.OpenSrsConfig, repos)
	mailboxOldImpl := mailboxold.NewMailboxServiceOld(log, repos, opensrsImpl)

	services := Services{
		EventsService:      events,
		AIService:          aiServiceImpl,
		CloudflareService:  cloudflareImpl,
		EmailFilterService: email_filter.NewEmailFilterService(),
		IMAPService:        imap.NewIMAPService(repos),
		MailboxService:     mailbox.NewMailboxService(repos),
		NamecheapService:   namecheapImpl,
		OpenSrsService:     opensrsImpl,

		MailboxServiceOld: mailboxOldImpl,
		DomainService:     domain.NewDomainService(repos, cloudflareImpl, namecheapImpl, mailboxOldImpl, opensrsImpl),
	}

	return &services, nil
}
