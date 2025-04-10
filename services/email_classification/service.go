package email_classification

import (
	"context"
	"errors"
	"fmt"

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

type EmailClassificationService struct {
	natsConn      *nats_internal.NATSConnections
	repositories  *repository.Repositories
	subscriptions []*nats.Subscription
}

func NewEmailClassificationService(
	natsConn *nats_internal.NATSConnections,
	repositories *repository.Repositories,
) interfaces.EmailProcessor {
	return &EmailClassificationService{
		natsConn:      natsConn,
		repositories:  repositories,
		subscriptions: make([]*nats.Subscription, 0),
	}
}

var SUBSCRIBED_SUBJECT = enum.EventEmailInboundClassify.String()

// Start begins listening for  events
func (s *EmailClassificationService) Start(ctx context.Context) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "EmailClassificationService.Start")
	defer spans.Finish()

	// Create a subscription for handling requests
	sub, err := s.natsConn.Conn.Subscribe(SUBSCRIBED_SUBJECT, func(msg *nats.Msg) {
		s.handleNatsMessage(ctx, msg)
	})
	if err != nil {
		spans.TraceError(err)
		return fmt.Errorf("failed to create subscription: %w", err)
	}

	// Keep track of subscription for cleanup
	s.subscriptions = append(s.subscriptions, sub)

	// Listen for context cancellation to clean up
	go func() {
		<-ctx.Done()
		for _, sub := range s.subscriptions {
			sub.Unsubscribe()
		}
	}()

	return nil
}

// Close gracefully shuts down the service
func (s *EmailClassificationService) Close() error {
	if s.natsConn != nil {
		s.natsConn.Close()
	}
	return nil
}

func (s *EmailClassificationService) handleNatsMessage(ctx context.Context, msg *nats.Msg) {
	ctx = utils.WithCustomContextFromNats(ctx, msg)
	spans, ctx := telemetry.StartListenerSpan(ctx, "EmailClassificationService.handleNatsMessage")
	defer spans.Finish()

	if msg == nil {
		spans.TraceError(errors.New("nil nats message"))
		return
	}
	spans.TagString("nats.subject", msg.Subject)
	spans.TagString("nats.reply", msg.Reply)

	resp := &pb.EmailClassificationResponse{}

	request := &pb.EmailClassificationRequest{}
	err := proto.Unmarshal(msg.Data, request)
	if err != nil || request == nil {
		errMsg := "Failed to parse request"
		resp.ErrorMessage = errMsg
		s.sendResponse(ctx, msg, resp)
		spans.TraceError(err)
		return
	}

	// Process the request
	resp = s.classifyEmail(ctx, request)
	if resp == nil {
		spans.TraceError(errors.New("empty response"))
		return
	}

	s.sendResponse(ctx, msg, resp)

}

func (s *EmailClassificationService) sendResponse(ctx context.Context, req *nats.Msg, resp *pb.EmailClassificationResponse) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "EmailClassificationService.sendResponse")
	defer spans.Finish()

	respMessage, err := proto.Marshal(resp)
	if err != nil {
		spans.TraceError(err)
		return
	}
	err = req.Respond(respMessage)
	if err != nil {
		spans.TraceError(err)
		return

	}
	return
}
