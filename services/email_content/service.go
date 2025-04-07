package email_content

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
	nats_internal "github.com/customeros/mailstack/internal/nats"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/telemetry"
	"github.com/customeros/mailstack/internal/utils"
	"github.com/customeros/mailstack/proto/pb"
)

type EmailContentService struct {
	natsConn     *nats_internal.NATSConnections
	repositories *repository.Repositories
	emlStorage   interfaces.StorageService
}

func NewEmailContentService(
	natsConn *nats_internal.NATSConnections,
	repositories *repository.Repositories,
	emlStorage interfaces.StorageService,
) interfaces.EmailProcessor {
	return &EmailContentService{
		natsConn:     natsConn,
		repositories: repositories,
		emlStorage:   emlStorage,
	}
}

var SUBSCRIBED_SUBJECT = enum.EventEmailInboundStored.String()

const (
	// queue group
	QUEUE_GROUP = "email-content-service"

	// consumer config
	CONSUMER_NAME         = "email-content-consumer"
	ACK_WAIT              = 30 * time.Second
	MAX_DELIVERY_ATTEMPTS = 5
	MAX_ACK_PENDING       = 1000
	FETCH_BATCH_SIZE      = 50
	MAX_FETCH_WAIT        = 500 * time.Millisecond
	ERR_BACKOFF           = 100 * time.Millisecond
)

// Start begins listening for raw email events and processing them
func (s *EmailContentService) Start(ctx context.Context) error {
	// Create durable consumer for processing emails
	_, err := s.natsConn.JS.AddConsumer(nats_internal.EMAIL_STREAM, &nats.ConsumerConfig{
		Durable:       CONSUMER_NAME,
		DeliverGroup:  QUEUE_GROUP,
		AckPolicy:     nats.AckExplicitPolicy,
		AckWait:       ACK_WAIT,
		MaxDeliver:    MAX_DELIVERY_ATTEMPTS,
		FilterSubject: SUBSCRIBED_SUBJECT,
		MaxAckPending: MAX_ACK_PENDING,
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
	go s.processEvents(ctx, sub)

	return nil
}

// processRawEmailEvents continuously processes raw email events
func (s *EmailContentService) processEvents(ctx context.Context, sub *nats.Subscription) {
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
func (s *EmailContentService) processBatch(ctx context.Context, sub *nats.Subscription) {
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
func (s *EmailContentService) handleFetchError(err error) {
	if err == nats.ErrTimeout {
		// No messages available, this is normal
		return
	}
	log.Printf("Fetch error: %v", err)
	time.Sleep(ERR_BACKOFF) // Small backoff on error
}

// processMessage processes a single email message
func (s *EmailContentService) processMessage(ctx context.Context, msg *nats.Msg) {
	ctx = utils.WithCustomContextFromNats(ctx, msg)
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailContentService.processMessage")
	defer spans.Finish()

	message := &pb.EmailStored{}
	err := proto.Unmarshal(msg.Data, message)
	if err != nil || message == nil {
		err := errors.New("Failed to parse message")
		spans.TraceError(err)
		s.handleProcessingError(ctx, msg, err)
		return
	}

	// Process the email
	err = s.processEmail(ctx, message)
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
func (s *EmailContentService) handleProcessingError(ctx context.Context, msg *nats.Msg, err error) {
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
func (s *EmailContentService) Close() error {
	if s.natsConn != nil {
		s.natsConn.Close()
	}
	return nil
}
