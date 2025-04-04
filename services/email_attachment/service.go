package email_attachment

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"

	"github.com/customeros/mailstack/dto"
	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/config"
	"github.com/customeros/mailstack/internal/enum"
	nats_internal "github.com/customeros/mailstack/internal/nats"
	"github.com/customeros/mailstack/internal/repository"
)

type EmailAttachmentService struct {
	config             *config.CustomerOSAPIConfig
	natsConn           *nats_internal.NATSConnections
	repositories       *repository.Repositories
	eventLoggerService interfaces.EventLoggerService
	subscriptions      []*nats.Subscription
}

func NewEmailAttachmentService(
	config *config.CustomerOSAPIConfig,
	natsConn *nats_internal.NATSConnections,
	repositories *repository.Repositories,
	eventLoggerService interfaces.EventLoggerService,
) interfaces.EmailProcessor {
	return &EmailAttachmentService{
		config:             config,
		natsConn:           natsConn,
		repositories:       repositories,
		eventLoggerService: eventLoggerService,
		subscriptions:      make([]*nats.Subscription, 0),
	}
}

var SUBSCRIBED_SUBJECT = enum.EventEmailInboundAttachments.String()

// Start begins listening for  events
func (s *EmailAttachmentService) Start(ctx context.Context) error {
	// Create a subscription for handling requests
	sub, err := s.natsConn.Conn.Subscribe(SUBSCRIBED_SUBJECT, func(msg *nats.Msg) {
		// Process the incoming request
		var request dto.ProcessAttachmentRequest

		event := s.eventLoggerService.NewEmailEventRecord(ctx)
		event.Event = request.EventType()

		err := json.Unmarshal(msg.Data, &request)
		if err != nil {
			errMsg := "Failed to parse request"
			resp := dto.ProcessAttachmentResponse{
				ErrorMessage: errMsg,
			}
			responseData, _ := json.Marshal(resp)
			msg.Respond(responseData)

			event.ErrorMessage = errMsg
			s.eventLoggerService.LogEmailEventToTimescale(ctx, event)
			return
		}
		event.EmailID = request.EmailID

		// Store original event in R2
		payloadKey, err := s.eventLoggerService.StoreEmailEventInR2(ctx, event.ID, msg.Data)
		if err != nil {
			err = fmt.Errorf("Failed to store event in R2: %v", err)
			event.ErrorMessage = err.Error()
			s.eventLoggerService.LogEmailEventToTimescale(ctx, event)
			return
		}
		event.PayloadKey = payloadKey

		// Process the request
		response := s.processAttachments(ctx, request)
		if response.ErrorMessage != "" {
			event.ErrorMessage = response.ErrorMessage
		}

		// Marshal and send response
		responseData, _ := json.Marshal(response)
		msg.Respond(responseData)
		s.eventLoggerService.LogEmailEventToTimescale(ctx, event)
	})
	if err != nil {
		return fmt.Errorf("failed to create subscription: %w", err)
	}

	// Keep track of subscription for cleanup
	s.subscriptions = append(s.subscriptions, sub)

	// Listen for context cancellation to clean up
	go func() {
		<-ctx.Done()
		for _, sub := range s.subscriptions {
			sub.Unsubscribe()
		}
	}()

	return nil
}

// Close gracefully shuts down the service
func (s *EmailAttachmentService) Close() error {
	if s.natsConn != nil {
		s.natsConn.Close()
	}
	return nil
}
