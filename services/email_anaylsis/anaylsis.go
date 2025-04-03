package email_analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/pkg/errors"

	"github.com/customeros/mailstack/dto"
	"github.com/customeros/mailstack/internal/telemetry"
)

type AskAIForEmailResponse struct {
	EmailData dto.AnalyzeEmailResponse `json:"emailData"`
}

func (s *EmailAnalysisService) getStructuredEmailBody(ctx context.Context, request dto.AnalyzeEmailRequest) (*dto.AnalyzeEmailResponse, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailAnalysisService.getStructuredEmailBody")
	defer spans.Finish()
	spans.LogObjectAsJson("request", request)

	// TODO migrate from HTTP call to NATS request/response

	payload, err := json.Marshal(request)
	if err != nil {
		spans.TraceError(err)
		return nil, errors.Wrap(err, "failed to marshal payload")
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.config.Url+"/internal/v1/askAIForEmail", bytes.NewBuffer(payload))
	if err != nil {
		spans.TraceError(err)
		return nil, errors.Wrap(err, "failed to create request")
	}

	req.Header.Set("X-Openline-API-KEY", s.config.ApiKey)
	req.Header.Set("X-Openline-Username", "matt@customeros.ai")
	req.Header.Set("X-Openline-Tenant", "customerosai")

	client := &http.Client{
		Timeout: 60 * time.Second,
	}
	// Execute the request
	resp, err := client.Do(req)
	if err != nil {
		spans.TraceError(err)
		return nil, errors.Wrap(err, "request failed")
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		spans.TraceError(err)
		return nil, errors.Wrap(err, "Unable to read response body")
	}

	// Check status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		spans.TraceError(err)
		return nil, fmt.Errorf("request failed with status code %d: %s", resp.StatusCode, string(body))
	}

	var response AskAIForEmailResponse
	if resp != nil {
		err := json.Unmarshal(body, &response)
		if err != nil {
			spans.TraceError(err)
			return nil, fmt.Errorf("failed to unmarshal response: %w", err)
		}
	}
	spans.LogObjectAsJson("response", response)

	return &response.EmailData, nil
}
