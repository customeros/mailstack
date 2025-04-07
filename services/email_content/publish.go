package email_content

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

func (s *EmailContentService) publishCompleted(ctx context.Context, email *pb.InboundEmailProcessingCompleted) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailContentService.publishCompleted")
	defer spans.Finish()

	data, err := proto.Marshal(email)
	if err != nil {
		spans.TraceError(err)
		return fmt.Errorf("failed to marshal stored email: %w", err)
	}

	// Publish to the stored subject
	_, err = s.natsConn.JS.Publish(enum.EventEmailInboundCompleted.String(), data)
	if err != nil {
		spans.TraceError(err)
		return fmt.Errorf("failed to publish completed email: %w", err)
	}

	spans.LogKV("completed", email.EmailId)
	return nil
}

// publishError publishes an error event
func (s *EmailContentService) publishError(ctx context.Context, msg *nats.Msg, err error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "EmailContentService.publishError")
	defer spans.Finish()

	errorEvent := &pb.ErrorEvent{
		Timestamp:    timestamppb.Now(),
		Subject:      msg.Subject,
		ErrorMessage: err.Error(),
		RawData:      msg.Data,
		Publisher:    pb_mappers.MailstackServiceToServiceName(enum.MailstackContentService),
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
