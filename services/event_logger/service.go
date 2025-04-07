package event_logger

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/enum"
	mailstack_errors "github.com/customeros/mailstack/internal/errors"
	"github.com/customeros/mailstack/internal/models"
	nats_internal "github.com/customeros/mailstack/internal/nats"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/telemetry"
	"github.com/customeros/mailstack/internal/utils"
	pb_mappers "github.com/customeros/mailstack/proto/mappers"
	"github.com/customeros/mailstack/proto/pb"
)

type EventLoggerService struct {
	natsConn      *nats_internal.NATSConnections
	repositories  *repository.Repositories
	eventsStorage interfaces.StorageService
}

func NewEventLoggerService(
	natsConn *nats_internal.NATSConnections,
	repos *repository.Repositories,
	eventsStorage interfaces.StorageService,
) interfaces.EmailProcessor {
	return &EventLoggerService{
		natsConn:      natsConn,
		repositories:  repos,
		eventsStorage: eventsStorage,
	}
}

var SUBSCRIBED_SUBJECT = "emails.>"

const (
	// queue group
	QUEUE_GROUP = "email-logger-service"

	// consumer config
	CONSUMER_NAME         = "email-logger-consumer"
	ACK_WAIT              = 30 * time.Second
	MAX_DELIVERY_ATTEMPTS = 5
	MAX_ACK_PENDING       = 100
	FETCH_BATCH_SIZE      = 50
	MAX_FETCH_WAIT        = 500 * time.Millisecond
	ERR_BACKOFF           = 100 * time.Millisecond
)

func (s *EventLoggerService) NewEmailEventRecord(ctx context.Context) *models.EmailEvent {
	spans, ctx := telemetry.StartServiceSpan(ctx, "eventLoggerService.NewEmailEventRecord")
	defer spans.Finish()

	// validate context
	var err error
	var errorMessage string

	tenant := utils.GetTenantFromContext(ctx)
	userID := utils.GetUserIdFromContext(ctx)

	if tenant == "" {
		err = mailstack_errors.ErrTenantMissing
		spans.TraceError(err)
		errorMessage = err.Error()
	}

	if userID == "" {
		err = mailstack_errors.ErrUserIdMissing
		spans.TraceError(err)
		errorMessage = err.Error()
	}

	return &models.EmailEvent{
		ID:           utils.GenerateNanoIDWithPrefix("event", 21),
		Timestamp:    utils.Now(),
		Tenant:       tenant,
		User:         userID,
		ErrorMessage: errorMessage, // Use the string variable instead of err.Error()
	}
}

// Start begins listening for raw email events and processing them
func (s *EventLoggerService) Start(ctx context.Context) error {
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
	go s.processEvent(ctx, sub)

	return nil
}

// processRawEmailEvents continuously processes raw email events
func (s *EventLoggerService) processEvent(ctx context.Context, sub *nats.Subscription) {
	log.Println("Email Logger Service started")
	for {
		select {
		case <-ctx.Done():
			log.Println("Email Logger Service shutting down")
			return
		default:
			s.processBatch(ctx, sub)
		}
	}
}

// processBatch fetches and processes a batch of messages
func (s *EventLoggerService) processBatch(ctx context.Context, sub *nats.Subscription) {
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
func (s *EventLoggerService) handleFetchError(err error) {
	if err == nats.ErrTimeout {
		// No messages available, this is normal
		return
	}
	log.Printf("Fetch error: %v", err)
	time.Sleep(ERR_BACKOFF) // Small backoff on error
}

// processMessage processes a single email message
func (s *EventLoggerService) processMessage(ctx context.Context, msg *nats.Msg) {
	ctx = utils.WithCustomContextFromNats(ctx, msg)
	spans, ctx := telemetry.StartServiceSpan(ctx, "EventLoggerService.processMessage")
	defer spans.Finish()

	// Extract the subject to determine message type
	subject := msg.Subject

	// Check if it's an error message
	if strings.HasPrefix(subject, "emails.errors.") {
		s.processErrorMessage(ctx, msg)
		msg.Ack()
		return
	}

	// Dispatch based on subject pattern
	switch {
	case strings.HasPrefix(subject, enum.EventEmailInboundReceivedIMAP.String()):
		s.processReceivedIMAPMessage(ctx, msg)

	case strings.HasPrefix(subject, enum.EventEmailInboundStored.String()):
		s.processStoredMessage(ctx, msg)

	case strings.HasPrefix(subject, enum.EventEmailInboundClassify.String()):
		s.processClassificationMessage(ctx, msg)

	// case strings.HasPrefix(subject, enum.EventEmailInboundClassifiedSkip.String()):
	//     s.processClassifiedSkipMessage(ctx, msg)
	//
	// case strings.HasPrefix(subject, enum.EventEmailInboundClassifiedBounce.String()):
	//     s.processClassifiedBounceMessage(ctx, msg)
	//
	// case strings.HasPrefix(subject, enum.EventEmailInboundClassifiedAutoresponder.String()):
	//     s.processClassifiedAutoresponderMessage(ctx, msg)

	case strings.HasPrefix(subject, enum.EventEmailInboundAnalysis.String()):
		s.processAnalysisMessage(ctx, msg)

	case strings.HasPrefix(subject, enum.EventEmailInboundAttachments.String()):
		s.processAttachmentsMessage(ctx, msg)

	case strings.HasPrefix(subject, enum.EventEmailInboundThread.String()):
		s.processThreadMessage(ctx, msg)

	case strings.HasPrefix(subject, enum.EventEmailInboundCompleted.String()):
		s.processInboundCompletedMessage(ctx, msg)

	default:
		err := errors.New("Unidentified message")
		spans.TraceError(err)
	}

	msg.Ack()
	return
}

// handleProcessingError deals with errors during email processing
func (s *EventLoggerService) handleProcessingError(ctx context.Context, msg *nats.Msg, err error) {
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
func (s *EventLoggerService) Close() error {
	if s.natsConn != nil {
		s.natsConn.Close()
	}
	return nil
}

func (s *EventLoggerService) publishError(ctx context.Context, msg *nats.Msg, err error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "EventLoggerService.publishError")
	defer spans.Finish()

	errorEvent := &pb.ErrorEvent{
		Timestamp:    timestamppb.Now(),
		Subject:      msg.Subject,
		ErrorMessage: err.Error(),
		RawData:      msg.Data,
		Publisher:    pb_mappers.MailstackServiceToServiceName(enum.MailstackEventLoggerService),
	}

	data, err := proto.Marshal(errorEvent)
	if err != nil {
		spans.TraceError(err)
		log.Printf("Failed to marshal error event: %v", err)
		return
	}

	_, pubErr := s.natsConn.JS.Publish(enum.EventEmailErrorLogger.String(), data)
	if pubErr != nil {
		spans.TraceError(pubErr)
		log.Printf("Failed to publish error event: %v", pubErr)
	}
}
