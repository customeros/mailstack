package email_storage

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

// PublishStoredEmail publishes the stored email to the next processing stage
func (s *EmailStorageService) publishStoredEmail(ctx context.Context, email *pb.EmailStored) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailStorageService.publishStoredEmail")
	defer spans.Finish()

	data, err := proto.Marshal(email)
	if err != nil {
		spans.TraceError(err)
		return fmt.Errorf("failed to marshal stored email: %w", err)
	}

	// Create message with headers
	msg := nats.NewMsg(enum.EventEmailInboundStored.String())
	msg.Data = data
	msg.Header.Set(interfaces.HEADER_TENANT, utils.GetTenantFromContext(ctx))
	msg.Header.Set(interfaces.HEADER_USERID, utils.GetUserIdFromContext(ctx))

	// Publish to the stored subject
	_, err = s.natsConn.JS.PublishMsg(msg)
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

	// Create message with headers
	newMsg := nats.NewMsg(enum.EventEmailInboundStored.String())
	newMsg.Data = data
	newMsg.Header.Set(interfaces.HEADER_TENANT, utils.GetTenantFromContext(ctx))
	newMsg.Header.Set(interfaces.HEADER_USERID, utils.GetUserIdFromContext(ctx))

	// Publish to the stored subject
	_, err = s.natsConn.JS.PublishMsg(newMsg)
	if err != nil {
		spans.TraceError(err)
		return
	}
}
