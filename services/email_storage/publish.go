package email_storage

import (
	"context"
	"fmt"
	"log"

	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/telemetry"
	pb_mappers "github.com/customeros/mailstack/proto/mappers"
	"github.com/customeros/mailstack/proto/pb"
)

// PublishStoredEmail publishes the stored email to the next processing stage
func (s *EmailStorageService) publishStoredEmail(ctx context.Context, email *pb.EmailStored) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailStorageService.publishStoredEmail")
	defer spans.Finish()

	// Convert to JSON
	data, err := proto.Marshal(email)
	if err != nil {
		spans.TraceError(err)
		return fmt.Errorf("failed to marshal stored email: %w", err)
	}

	// Publish to the stored subject
	_, err = s.natsConn.JS.Publish(enum.EventEmailInboundStored.String(), data)
	if err != nil {
		spans.TraceError(err)
		return fmt.Errorf("failed to publish stored email: %w", err)
	}

	return nil
}

// publishError publishes an error event
func (s *EmailStorageService) publishError(ctx context.Context, msg *nats.Msg, err error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "EmailStorageService.publishError")
	defer spans.Finish()

	errorEvent := &pb.ErrorEvent{
		Timestamp:    timestamppb.Now(),
		Subject:      msg.Subject,
		ErrorMessage: err.Error(),
		RawData:      msg.Data,
		Publisher:    pb_mappers.MailstackServiceToServiceName(enum.MailstackStorageService),
	}

	data, err := proto.Marshal(errorEvent)
	if err != nil {
		spans.TraceError(err)
		log.Printf("Failed to marshal error event: %v", err)
		return
	}

	_, pubErr := s.natsConn.JS.Publish(enum.EventEmailErrorInbound.String(), data)
	if pubErr != nil {
		spans.TraceError(pubErr)
		log.Printf("Failed to publish error event: %v", pubErr)
	}
}
