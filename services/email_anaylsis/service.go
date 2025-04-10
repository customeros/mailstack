package email_analysis

import (
	"context"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/config"
	"github.com/customeros/mailstack/internal/enum"
	nats_internal "github.com/customeros/mailstack/internal/nats"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/telemetry"
	"github.com/customeros/mailstack/internal/utils"
	"github.com/customeros/mailstack/proto/pb"
)

type EmailAnalysisService struct {
	config        *config.CustomerOSAPIConfig
	natsConn      *nats_internal.NATSConnections
	repositories  *repository.Repositories
	subscriptions []*nats.Subscription
}

func NewEmailAnalysisService(
	config *config.CustomerOSAPIConfig,
	natsConn *nats_internal.NATSConnections,
	repositories *repository.Repositories,
) interfaces.EmailProcessor {
	return &EmailAnalysisService{
		config:        config,
		natsConn:      natsConn,
		repositories:  repositories,
		subscriptions: make([]*nats.Subscription, 0),
	}
}

var SUBSCRIBED_SUBJECT = enum.EventEmailInboundAnalysis.String()

// Start begins listening for  events
func (s *EmailAnalysisService) Start(ctx context.Context) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "EmailAnalysisService.Start")
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
func (s *EmailAnalysisService) Close() error {
	if s.natsConn != nil {
		s.natsConn.Close()
	}
	return nil
}

func (s *EmailAnalysisService) handleNatsMessage(ctx context.Context, msg *nats.Msg) {
	ctx = utils.WithCustomContextFromNats(ctx, msg)
	spans, ctx := telemetry.StartListenerSpan(ctx, "EmailAnalysisService.handleNatsMessage")
	defer spans.Finish()

	if msg == nil {
		spans.TraceError(errors.New("nil nats message"))
		return
	}
	spans.TagString("nats.subject", msg.Subject)
	spans.TagString("nats.reply", msg.Reply)

	resp := &pb.AnalyzeEmailResponse{}

	request := &pb.AnalyzeEmailRequest{}
	err := proto.Unmarshal(msg.Data, request)
	if err != nil {
		errMsg := "Failed to parse request"
		resp.ErrorMessage = errMsg
		s.sendResponse(ctx, msg, resp)
		spans.TraceError(err)
		return
	}

	resp = s.processRequestForStructuredBody(ctx, request)
	if resp == nil {
		spans.TraceError(errors.New("empty response"))
		return
	}

	s.sendResponse(ctx, msg, resp)

}

func (s *EmailAnalysisService) sendResponse(ctx context.Context, req *nats.Msg, resp *pb.AnalyzeEmailResponse) {
	spans, _ := telemetry.StartServiceSpan(ctx, "EmailAnalysisService.sendResponse")
	defer spans.Finish()

	respMessage, err := proto.Marshal(resp)
	if err != nil {
		spans.TraceError(err)
		return
	}
	req.Respond(respMessage)
}
