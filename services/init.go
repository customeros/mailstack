package services

import (
	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/logger"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/services/email_filter"
	"github.com/customeros/mailstack/services/events"
	"github.com/customeros/mailstack/services/imap"
)

type Services struct {
	EventsService      *events.EventsService
	EmailFilterService interfaces.EmailFilterService
	IMAPService        interfaces.IMAPService
}

func InitServices(rabbitmqURL string, log logger.Logger, repos *repository.Repositories) (*Services, error) {
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

	services := Services{
		EventsService:      events,
		EmailFilterService: email_filter.NewEmailFilterService(),
		IMAPService:        imap.NewIMAPService(repos),
	}

	return &services, nil
}
