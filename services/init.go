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
	"github.com/customeros/mailstack/services/namecheap"
	"github.com/customeros/mailstack/services/opensrs"
)

type Services struct {
	EventsService      *events.EventsService
	AIService          interfaces.AIService
	EmailFilterService interfaces.EmailFilterService
	IMAPService        interfaces.IMAPService
	NamecheapService   interfaces.NamecheapService
	OpenSrsService     interfaces.OpenSrsService
	CloudflareService  interfaces.CloudflareService
	MailboxService     interfaces.MailboxService
	DomainService      interfaces.DomainService
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
	mailboxImpl := mailbox.NewMailboxService(log, repos, opensrsImpl)

	services := Services{
		EventsService:      events,
		AIService:          aiServiceImpl,
		EmailFilterService: email_filter.NewEmailFilterService(),
		IMAPService:        imap.NewIMAPService(repos),
		NamecheapService:   namecheapImpl,
		OpenSrsService:     opensrsImpl,
		CloudflareService:  cloudflareImpl,
		MailboxService:     mailboxImpl,
		DomainService:      domain.NewDomainService(repos, cloudflareImpl, namecheapImpl, mailboxImpl, opensrsImpl),
	}

	return &services, nil
}
