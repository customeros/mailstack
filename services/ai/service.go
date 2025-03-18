package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"

	"github.com/customeros/mailstack/dto"
	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/config"
	"github.com/customeros/mailstack/internal/tracing"
)

type aiService struct {
	CustomerOSAPIConfig *config.CustomerOSAPIConfig
}

func NewAIService(config *config.CustomerOSAPIConfig) interfaces.AIService {
	return &aiService{
		CustomerOSAPIConfig: config,
	}
}

func (s *aiService) GetStructuredEmailBody(ctx context.Context, request dto.StructuredEmailRequest) (*dto.StructuredEmailResponse, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "aiService.GetStructuredEmailBody")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)
	tracing.LogObjectAsJson(span, "request", request)

	payload, err := json.Marshal(request)
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, errors.Wrap(err, "failed to marshal payload")
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.CustomerOSAPIConfig.Url+"/internal/v1/askAIForEmail", bytes.NewBuffer(payload))
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, errors.Wrap(err, "failed to create request")
	}

	req.Header.Set("X-Openline-API-KEY", s.CustomerOSAPIConfig.ApiKey)
	req.Header.Set("X-Openline-Username", "matt@customeros.ai")
	req.Header.Set("X-Openline-Tenant", "customerosai")

	client := &http.Client{
		Timeout: 60 * time.Second,
	}
	// Execute the request
	resp, err := client.Do(req)
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, errors.Wrap(err, "request failed")
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, errors.Wrap(err, "Unable to read response body")
	}

	// Check status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		tracing.TraceErr(span, err)
		return nil, fmt.Errorf("request failed with status code %d: %s", resp.StatusCode, string(body))
	}

	var response dto.StructuredEmailResponse
	if resp != nil {
		err := json.Unmarshal(body, &response)
		if err != nil {
			tracing.TraceErr(span, err)
			return nil, fmt.Errorf("failed to unmarshal response: %w", err)
		}
	}
	tracing.LogObjectAsJson(span, "response", response)

	return &response, nil
}
