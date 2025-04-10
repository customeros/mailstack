package email_thread

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

type EmailThreadingService struct {
	natsConn      *nats_internal.NATSConnections
	repositories  *repository.Repositories
	subscriptions []*nats.Subscription
}

func NewEmailThreadingService(
	natsConn *nats_internal.NATSConnections,
	repositories *repository.Repositories,
) interfaces.EmailProcessor {
	return &EmailThreadingService{
		natsConn:      natsConn,
		repositories:  repositories,
		subscriptions: make([]*nats.Subscription, 0),
	}
}

var SUBSCRIBED_SUBJECT = enum.EventEmailInboundThread.String()

// Start begins listening for  events
func (s *EmailThreadingService) Start(ctx context.Context) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "EmailThreadingService.Start")
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
func (s *EmailThreadingService) Close() error {
	if s.natsConn != nil {
		s.natsConn.Close()
	}
	return nil
}

func (s *EmailThreadingService) handleNatsMessage(ctx context.Context, msg *nats.Msg) {
	ctx = utils.WithCustomContextFromNats(ctx, msg)
	spans, ctx := telemetry.StartListenerSpan(ctx, "EmailThreadingService.handleNatsMessage")
	defer spans.Finish()

	if msg == nil {
		spans.TraceError(errors.New("nil nats message"))
		return
	}
	spans.TagString("nats.subject", msg.Subject)
	spans.TagString("nats.reply", msg.Reply)

	resp := &pb.AttachToThreadResponse{}
	request := &pb.AttachToThreadRequest{}

	if err := proto.Unmarshal(msg.Data, request); err != nil {
		errMsg := "Failed to parse request"
		resp.ErrorMessage = errMsg
		s.sendResponse(ctx, msg, resp)
		spans.TraceError(err)
		return
	}

	// Process the request
	response := s.attachToThread(ctx, request)
	if response == nil {
		spans.TraceError(errors.New("empty response"))
		return
	}

	s.sendResponse(ctx, msg, response)
}

func (s *EmailThreadingService) sendResponse(ctx context.Context, req *nats.Msg, resp *pb.AttachToThreadResponse) {
	spans, _ := telemetry.StartServiceSpan(ctx, "EmailThreadingService.sendResponse")
	defer spans.Finish()

	respMessage, err := proto.Marshal(resp)
	if err != nil {
		spans.TraceError(err)
		return
	}
	req.Respond(respMessage)
}
