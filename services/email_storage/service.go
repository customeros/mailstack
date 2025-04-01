package email_storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/customeros/mailstack/dto"
	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/enum"
	nats_internal "github.com/customeros/mailstack/internal/nats"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/telemetry"
	"github.com/customeros/mailstack/internal/utils"
	"github.com/customeros/mailstack/services/email_processor"
)

type emailStorageService struct {
	natsConn       *nats_internal.NATSConnections
	repositories   *repository.Repositories
	imapProcessor  *email_processor.ImapProcessor
	storageService interfaces.StorageService
	imapService    interfaces.IMAPService
}

func NewEmailStorageService(
	natsConn *nats_internal.NATSConnections,
	repositories *repository.Repositories,
	imapProcessor *email_processor.ImapProcessor,
	storageService interfaces.StorageService,
	imapService interfaces.IMAPService,
) interfaces.EmailStorageService {
	return &emailStorageService{
		natsConn:       natsConn,
		repositories:   repositories,
		imapProcessor:  imapProcessor,
		storageService: storageService,
		imapService:    imapService,
	}
}

const (
	// queue group
	QUEUE_GROUP = "storage-service"

	// consumer config
	CONSUMER_NAME         = "storage-consumer"
	ACK_WAIT              = 30 * time.Second
	MAX_DELIVERY_ATTEMPTS = 5
	MAX_ACK_PENDING       = 100
	FETCH_BATCH_SIZE      = 50
	MAX_FETCH_WAIT        = 500 * time.Millisecond
	ERR_BACKOFF           = 100 * time.Millisecond
)

// Start begins listening for raw email events and processing them
func (s *emailStorageService) Start(ctx context.Context) error {
	// Create durable consumer for processing emails
	_, err := s.natsConn.JS.AddConsumer(nats_internal.EMAIL_STREAM, &nats.ConsumerConfig{
		Durable:       CONSUMER_NAME,
		AckPolicy:     nats.AckExplicitPolicy,
		AckWait:       ACK_WAIT,
		MaxDeliver:    MAX_DELIVERY_ATTEMPTS,
		FilterSubject: enum.EventEmailInboundReceivedIMAP.String(),
		MaxAckPending: MAX_ACK_PENDING,
	})
	if err != nil {
		return fmt.Errorf("failed to create consumer: %w", err)
	}

	// Create pull subscription
	sub, err := s.natsConn.JS.PullSubscribe(
		enum.EventEmailInboundReceivedIMAP.String(),
		QUEUE_GROUP,
		nats.Bind(nats_internal.EMAIL_STREAM, CONSUMER_NAME),
	)
	if err != nil {
		return fmt.Errorf("failed to create subscription: %w", err)
	}

	// Start processing
	go s.processRawEmailEvents(ctx, sub)

	return nil
}

// processEmails continuously processes raw email events
func (s *emailStorageService) processRawEmailEvents(ctx context.Context, sub *nats.Subscription) {
	log.Println("Email Storage Service started")

	for {
		select {
		case <-ctx.Done():
			log.Println("Email Storage Service shutting down")
			return
		default:
			// Fetch messages batch
			msgs, err := sub.Fetch(FETCH_BATCH_SIZE, nats.MaxWait(MAX_FETCH_WAIT))
			if err != nil {
				if err == nats.ErrTimeout {
					// No messages available, this is normal
					continue
				}
				log.Printf("Fetch error: %v", err)
				time.Sleep(ERR_BACKOFF) // Small backoff on error
				continue
			}

			for _, msg := range msgs {
				// Parse the raw message
				var rawEmail dto.EmailReceivedIMAP
				if err := json.Unmarshal(msg.Data, &rawEmail); err != nil {
					log.Printf("Failed to unmarshal inbound.email.received.imap: %v", err)
					msg.Ack() // Ack malformed messages to avoid redelivery
					continue
				}

				// Process the email
				err := s.HandleIMAPEmail(ctx, rawEmail)

				if err != nil {
					log.Printf("Processing error: %v", err)
					metadata, _ := msg.Metadata()

					// Check if we should retry
					if metadata.NumDelivered <= uint64(MAX_DELIVERY_ATTEMPTS) {
						// Negative acknowledgment triggers redelivery
						msg.Nak()
					} else {
						// Max retries reached, acknowledge but publish to dead letter
						msg.Ack()
						s.publishError(ctx, msg.Data, err)
					}
				} else {
					// Successfully processed
					msg.Ack()
				}
			}
		}
	}
}

// PublishStoredEmail publishes the stored email to the next processing stage
func (s *emailStorageService) PublishStoredEmail(ctx context.Context, email *dto.EmailRecord) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "EmailStorageService.publishStoredEmail")
	defer spans.Finish()

	// Create a simplified version for the next stage
	storedEmail := dto.StoredEmail{
		ID:             email.ID,
		EmailKey:       email.EmailKey,
		MailboxID:      email.MailboxID,
		MessageID:      email.MessageID,
		ThreadID:       email.ThreadID,
		FromAddress:    email.FromAddress,
		FromDomain:     email.FromDomain,
		ToAddresses:    email.ToAddresses,
		CcAddresses:    email.CcAddresses,
		Subject:        email.Subject,
		HasAttachment:  email.HasAttachment,
		SentAt:         email.SentAt,
		ReceivedAt:     email.ReceivedAt,
		Classification: string(email.Classification),
	}

	// Convert to JSON
	data, err := json.Marshal(storedEmail)
	if err != nil {
		spans.TraceError(err)
		return fmt.Errorf("failed to marshal stored email: %w", err)
	}

	// Publish to the stored subject
	_, err = s.natsConn.JS.Publish("emails.inbound.stored", data)
	if err != nil {
		spans.TraceError(err)
		return fmt.Errorf("failed to publish stored email: %w", err)
	}

	spans.LogKV("published", email.ID)
	return nil
}

// publishError publishes an error event
func (s *emailStorageService) publishError(ctx context.Context, rawData []byte, err error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "EmailStorageService.publishError")
	defer spans.Finish()

	errorEvent := struct {
		Timestamp time.Time `json:"timestamp"`
		Error     string    `json:"error"`
		RawData   []byte    `json:"raw_data"`
	}{
		Timestamp: time.Now(),
		Error:     err.Error(),
		RawData:   rawData,
	}

	data, jsonErr := json.Marshal(errorEvent)
	if jsonErr != nil {
		spans.TraceError(jsonErr)
		log.Printf("Failed to marshal error event: %v", jsonErr)
		return
	}

	_, pubErr := s.natsConn.JS.Publish("emails.inbound.errors", data)
	if pubErr != nil {
		spans.TraceError(pubErr)
		log.Printf("Failed to publish error event: %v", pubErr)
	}

	// Also log to timescale
	s.logErrorEvent(ctx, errorEvent)
}

// logEmailEvent logs an event to TimescaleDB for audit and analytics
func (s *emailStorageService) logEmailEvent(ctx context.Context, eventType string, email *dto.EmailRecord) error {
	// Create the event record
	event := models.EmailEvent{
		Timestamp:      utils.Now(),
		Tenant:         utils.GetTenantFromContext(ctx),
		EventType:      eventType,
		EmailID:        email.ID,
		MailboxID:      email.MailboxID,
		MessageID:      email.MessageID,
		ThreadID:       email.ThreadID,
		FromAddress:    email.FromAddress,
		FromUser:       email.FromUser,
		FromDomain:     email.FromDomain,
		Recipients:     email.Recipients(),
		EmailKey:       email.EmailKey,
		Subject:        email.Subject,
		Direction:      string(email.Direction),
		Classification: string(email.Classification),
		SentAt:         email.SentAt,
		ReceivedAt:     email.ReceivedAt,
	}

	// Save to database
	return s.repositories.EmailEventRepository.Create(ctx, &event)
}

// logErrorEvent logs an error event to TimescaleDB
func (s *emailStorageService) logErrorEvent(ctx context.Context, errorEvent interface{}) {
	// Implementation depends on your repository structure
	// This is a placeholder
	log.Printf("Logging error event to TimescaleDB: %+v", errorEvent)
}

// Close gracefully shuts down the service
func (s *emailStorageService) Close() error {
	if s.natsConn != nil {
		s.natsConn.Close()
	}
	return nil
}
