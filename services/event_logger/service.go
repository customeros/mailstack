package event_logger

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

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
		ErrorMessage: errorMessage,
	}
}

// Start begins listening for raw email events and processing them
func (s *EventLoggerService) Start(ctx context.Context) error {
	// Subscribe to all standard request/reply messages
	_, err := s.natsConn.Conn.Subscribe(SUBSCRIBED_SUBJECT, func(msg *nats.Msg) {
		spans, ctx := telemetry.StartServiceSpan(ctx, "EventLoggerService.setupNonPersistedSubscriptions")
		defer spans.Finish()

		s.processMessage(ctx, msg)
	})
	if err != nil {
		return fmt.Errorf("failed to create standard subscription: %w", err)
	}

	log.Println("Email Logger Service started standard NATS subscriptions")
	return nil
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

	case strings.HasPrefix(subject, enum.EventEmailInboundClassifiedSkip.String()):
		s.processSkipInboundProcessing(ctx, msg)

	default:
		err := errors.New("Unidentified message")
		spans.TraceError(err)
	}

	return
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
