package email_storage

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
	nats_internal "github.com/customeros/mailstack/internal/nats"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/telemetry"
	"github.com/customeros/mailstack/internal/utils"
	"github.com/customeros/mailstack/proto/pb"
)

type EmailStorageService struct {
	natsConn     *nats_internal.NATSConnections
	repositories *repository.Repositories
	imapService  interfaces.IMAPService
	emlStorage   interfaces.StorageService
}

func NewEmailStorageService(
	natsConn *nats_internal.NATSConnections,
	repositories *repository.Repositories,
	imapService interfaces.IMAPService,
	emlStorage interfaces.StorageService,
) interfaces.EmailProcessor {
	return &EmailStorageService{
		natsConn:     natsConn,
		repositories: repositories,
		imapService:  imapService,
		emlStorage:   emlStorage,
	}
}

var SUBSCRIBED_SUBJECT = enum.EventEmailInboundReceivedIMAP.String()

const (
	// queue group
	QUEUE_GROUP = "email-storage-service"

	// consumer config
	CONSUMER_NAME         = "email-storage-consumer"
	ACK_WAIT              = 30 * time.Second
	MAX_DELIVERY_ATTEMPTS = 5
	MAX_ACK_PENDING       = 100
	FETCH_BATCH_SIZE      = 50
	MAX_FETCH_WAIT        = 500 * time.Millisecond
	ERR_BACKOFF           = 100 * time.Millisecond
)

func (s *EmailStorageService) NewEmailLog() *models.EmailLog {
	return &models.EmailLog{
		ID:        utils.GenerateNanoIDWithPrefix("email", 21),
		Direction: enum.EmailDirectionInbound,
		Status:    enum.EmailStatusReceived,
	}
}

// Start begins listening for raw email events and processing them
func (s *EmailStorageService) Start(ctx context.Context) error {
	// Create durable consumer for processing emails
	_, err := s.natsConn.JS.AddConsumer(nats_internal.EMAIL_STREAM, &nats.ConsumerConfig{
		Durable:       CONSUMER_NAME,
		DeliverGroup:  QUEUE_GROUP,
		AckPolicy:     nats.AckExplicitPolicy,
		AckWait:       ACK_WAIT,
		MaxDeliver:    MAX_DELIVERY_ATTEMPTS,
		FilterSubject: SUBSCRIBED_SUBJECT,
		MaxAckPending: MAX_ACK_PENDING,
		DeliverPolicy: nats.DeliverAllPolicy,
	})
	if err != nil {
		return fmt.Errorf("failed to create consumer: %w", err)
	}

	// Create pull subscription
	sub, err := s.natsConn.JS.PullSubscribe(
		SUBSCRIBED_SUBJECT,
		CONSUMER_NAME,
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
func (s *EmailStorageService) processRawEmailEvents(ctx context.Context, sub *nats.Subscription) {
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
func (s *EmailStorageService) processBatch(ctx context.Context, sub *nats.Subscription) {
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
func (s *EmailStorageService) handleFetchError(err error) {
	if err == nats.ErrTimeout {
		// No messages available, this is normal
		return
	}
	log.Printf("Fetch error: %v", err)
	time.Sleep(ERR_BACKOFF) // Small backoff on error
}

// processMessage processes a single email message
func (s *EmailStorageService) processMessage(ctx context.Context, msg *nats.Msg) {
	ctx = utils.WithCustomContextFromNats(ctx, msg)
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailStorageService.processMessage")
	defer spans.Finish()

	if msg == nil {
		spans.TraceError(errors.New("nil nats message"))
		return
	}
	spans.TagString("nats.subject", msg.Subject)
	spans.TagString("nats.reply", msg.Reply)

	message := &pb.EmailReceivedIMAP{}
	err := proto.Unmarshal(msg.Data, message)
	if err != nil || message == nil {
		err := errors.New("Failed to parse message")
		spans.TraceError(err)
		s.handleProcessingError(ctx, msg, err)
		return
	}

	// Process the email
	s.handleIMAPEmail(ctx, message)
	if err != nil {
		if !strings.Contains(err.Error(), "skipping") {
			spans.TraceError(err)
		}
		s.handleProcessingError(ctx, msg, err)
		return
	}

	msg.Ack()
	return
}

// handleProcessingError deals with errors during email processing
func (s *EmailStorageService) handleProcessingError(ctx context.Context, msg *nats.Msg, err error) {
	metadata, _ := msg.Metadata()

	// Check if we should retry
	if metadata.NumDelivered <= uint64(MAX_DELIVERY_ATTEMPTS) {
		// Negative acknowledgment triggers redelivery
		msg.Nak()
	} else {
		// Max retries reached, acknowledge but publish to dead letter
		msg.Ack()
		s.publishError(ctx, msg, err)
	}
}

// Close gracefully shuts down the service
func (s *EmailStorageService) Close() error {
	if s.natsConn != nil {
		s.natsConn.Close()
	}
	return nil
}
