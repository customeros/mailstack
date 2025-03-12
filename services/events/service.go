package events

import (
	"fmt"

	"github.com/customeros/mailstack/internal/logger"
)

type EventsService struct {
	Publisher *RabbitMQPublisher
}

func NewEventsService(rabbitmqURL string, log logger.Logger, publisherConfig *PublisherConfig) (*EventsService, error) {
	publisher, err := NewRabbitMQPublisher(rabbitmqURL, log, publisherConfig)
	if err != nil {
		return nil, err
	}

	return &EventsService{
		Publisher: publisher,
	}, nil
}

func (s *EventsService) Close() error {
	var errs []error

	if s.Publisher != nil {
		if err := s.Publisher.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing events service: %v", errs)
	}

	return nil
}
