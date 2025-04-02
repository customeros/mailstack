package email_storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/customeros/mailstack/dto"
	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
	nats_internal "github.com/customeros/mailstack/internal/nats"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/telemetry"
	"github.com/customeros/mailstack/internal/utils"
)

type emailStorageService struct {
	natsConn           *nats_internal.NATSConnections
	repositories       *repository.Repositories
	eventLoggerService interfaces.EventLoggerService
	storageService     interfaces.StorageService
	imapService        interfaces.IMAPService
}

func NewEmailStorageService(
	natsConn *nats_internal.NATSConnections,
	repositories *repository.Repositories,
	eventLoggerService interfaces.EventLoggerService,
	storageService interfaces.StorageService,
	imapService interfaces.IMAPService,
) interfaces.EmailStorageService {
	return &emailStorageService{
		natsConn:           natsConn,
		repositories:       repositories,
		eventLoggerService: eventLoggerService,
		storageService:     storageService,
		imapService:        imapService,
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

func (s *emailStorageService) NewEmailLog() *models.EmailLog {
	return &models.EmailLog{
		ID:        utils.GenerateNanoIDWithPrefix("email", 21),
		Direction: enum.EmailDirectionInbound,
		Status:    enum.EmailStatusReceived,
	}
}

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

// processRawEmailEvents continuously processes raw email events
func (s *emailStorageService) processRawEmailEvents(ctx context.Context, sub *nats.Subscription) {
	log.Println("Email Storage Service started")
	for {
		select {
		case <-ctx.Done():
			log.Println("Email Storage Service shutting down")
			return
		default:
			s.processBatch(ctx, sub)
		}
	}
}

// processBatch fetches and processes a batch of messages
func (s *emailStorageService) processBatch(ctx context.Context, sub *nats.Subscription) {
	// Fetch messages batch
	msgs, err := sub.Fetch(FETCH_BATCH_SIZE, nats.MaxWait(MAX_FETCH_WAIT))
	if err != nil {
		s.handleFetchError(err)
		return
	}

	for _, msg := range msgs {
		msgCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		s.processMessage(msgCtx, msg)
		cancel()
	}
}

// handleFetchError handles errors that occur during message fetching
func (s *emailStorageService) handleFetchError(err error) {
	if err == nats.ErrTimeout {
		// No messages available, this is normal
		return
	}
	log.Printf("Fetch error: %v", err)
	time.Sleep(ERR_BACKOFF) // Small backoff on error
}

// processMessage processes a single email message
func (s *emailStorageService) processMessage(ctx context.Context, msg *nats.Msg) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailStorageService.processMessage")
	defer spans.Finish()

	var rawEmail dto.EmailReceivedIMAP

	event := s.eventLoggerService.NewEmailEventRecord(ctx)
	event.Event = rawEmail.EventType()
	if event.ErrorMessage != "" {
		msg.Ack() // Ack malformed messages to avoid redelivery
		s.eventLoggerService.LogEmailEventToTimescale(ctx, event)
		return
	}

	// Parse the raw message
	if err := json.Unmarshal(msg.Data, &rawEmail); err != nil {
		err = fmt.Errorf("Failed to unmarshal inbound.email.received.imap: %v", err)
		msg.Ack() // Ack malformed messages to avoid redelivery
		event.ErrorMessage = err.Error()
		s.eventLoggerService.LogEmailEventToTimescale(ctx, event)
		spans.TraceError(err)
		return
	}
	event.MailboxID = rawEmail.MailboxID

	// Store original event in R2
	payloadKey, err := s.eventLoggerService.StoreEmailEventInR2(ctx, event.ID, msg.Data)
	if err != nil {
		err = fmt.Errorf("Failed to store event in R2: %v", err)
		event.ErrorMessage = err.Error()
		s.eventLoggerService.LogEmailEventToTimescale(ctx, event)
		s.handleProcessingError(ctx, msg, err)
		spans.TraceError(err)
		return
	}
	event.PayloadKey = payloadKey

	// Process the email
	s.handleIMAPEmail(ctx, rawEmail, event)
	s.eventLoggerService.LogEmailEventToTimescale(ctx, event)

	if event.ErrorMessage != "" {
		s.handleProcessingError(ctx, msg, err)
		if !strings.Contains(event.ErrorMessage, "skipping") {
			spans.TraceError(err)
		}
		return
	}

	msg.Ack()
	return
}

// handleProcessingError deals with errors during email processing
func (s *emailStorageService) handleProcessingError(ctx context.Context, msg *nats.Msg, err error) {
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
}

// Close gracefully shuts down the service
func (s *emailStorageService) Close() error {
	if s.natsConn != nil {
		s.natsConn.Close()
	}
	return nil
}
