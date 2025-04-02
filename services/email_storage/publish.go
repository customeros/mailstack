package email_storage

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

// PublishStoredEmail publishes the stored email to the next processing stage
func (s *emailStorageService) publishStoredEmail(ctx context.Context, email *dto.EmailStored) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailStorageService.publishStoredEmail")
	defer spans.Finish()

	// Convert to JSON
	data, err := json.Marshal(email)
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

	spans.LogKV("published", email.ID)
	return nil
}

// publishError publishes an error event
func (s *emailStorageService) publishError(ctx context.Context, rawData []byte, err error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "EmailStorageService.publishError")
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
