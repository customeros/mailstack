package email_content

import (
	"context"
	"fmt"
	"github.com/customeros/mailstack/interfaces"
	"log"

	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/telemetry"
	"github.com/customeros/mailstack/internal/utils"
	pb_mappers "github.com/customeros/mailstack/proto/mappers"
	"github.com/customeros/mailstack/proto/pb"
)

func (s *EmailContentService) publishCompleted(ctx context.Context, email *pb.InboundEmailProcessingCompleted) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailContentService.publishCompleted")
	defer spans.Finish()

	data, err := proto.Marshal(email)
	if err != nil {
		spans.TraceError(err)
		return fmt.Errorf("failed to marshal completed notification: %w", err)
	}

	// Create message with headers
	msg := nats.NewMsg(enum.EventEmailInboundCompleted.String())
	msg.Data = data
	msg.Header.Set(interfaces.HEADER_TENANT, utils.GetTenantFromContext(ctx))
	msg.Header.Set(interfaces.HEADER_USERID, utils.GetUserIdFromContext(ctx))

	// Publish to the stored subject
	_, err = s.natsConn.JS.PublishMsg(msg)
	if err != nil {
		spans.TraceError(err)
		return fmt.Errorf("failed to publish completed email event: %w", err)
	}

	spans.LogKV("completed", email.EmailId)
	return nil
}

func (s *EmailContentService) publishSkipNotification(ctx context.Context, email *pb.SkipInboundProcessing) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailContentService.publishSkipNotification")
	defer spans.Finish()

	data, err := proto.Marshal(email)
	if err != nil {
		spans.TraceError(err)
		return fmt.Errorf("failed to marshal skip notification: %w", err)
	}

	// Create message with headers
	msg := nats.NewMsg(enum.EventEmailInboundClassifiedSkip.String())
	msg.Data = data
	msg.Header.Set(interfaces.HEADER_TENANT, utils.GetTenantFromContext(ctx))
	msg.Header.Set(interfaces.HEADER_USERID, utils.GetUserIdFromContext(ctx))

	// Publish to the stored subject
	err = s.natsConn.Conn.PublishMsg(msg)
	if err != nil {
		spans.TraceError(err)
		return fmt.Errorf("failed to publish skip notification event: %w", err)
	}

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

	errMsg := nats.NewMsg(enum.EventEmailErrorInbound.String())
	errMsg.Data = data
	errMsg.Header.Set(interfaces.HEADER_TENANT, utils.GetTenantFromContext(ctx))
	errMsg.Header.Set(interfaces.HEADER_USERID, utils.GetUserIdFromContext(ctx))

	// Publish to the stored subject
	_, err = s.natsConn.JS.PublishMsg(errMsg)
	if err != nil {
		spans.TraceError(err)
		return
	}

	return
}
