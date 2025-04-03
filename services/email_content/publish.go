package email_content

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/customeros/mailstack/dto"
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/telemetry"
)

func (s *EmailContentService) publishCompleted(ctx context.Context, email *dto.InboundEmailProcessingCompleted) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailContentService.publishCompleted")
	defer spans.Finish()

	// Convert to JSON
	data, err := json.Marshal(email)
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

	spans.LogKV("completed", email.EmailID)
	return nil
}

// publishError publishes an error event
func (s *EmailContentService) publishError(ctx context.Context, rawData []byte, err error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailContentService.publishError")
	defer spans.Finish()

	errorEvent := struct {
		Timestamp time.Time `json:"timestamp"`
		Error     string    `json:"error"`
		RawData   []byte    `json:"raw_data"`
	}{
		Timestamp: time.Now(),
		Error:     err.Error(),
		RawData:   rawData,
	}

	data, jsonErr := json.Marshal(errorEvent)
	if jsonErr != nil {
		spans.TraceError(jsonErr)
		return
	}

	_, pubErr := s.natsConn.JS.Publish(enum.EventEmailInboundError.String(), data)
	if pubErr != nil {
		spans.TraceError(pubErr)
		log.Printf("Failed to publish error event: %v", pubErr)
	}
}
