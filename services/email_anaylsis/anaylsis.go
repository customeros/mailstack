package email_analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/customeros/mailstack/dto"
	"github.com/customeros/mailstack/internal/telemetry"
)

type AskAIForEmailResponse struct {
	EmailData dto.AnalyzeEmailResponse `json:"emailData"`
}

func (s *EmailAnalysisService) getStructuredEmailBody(ctx context.Context, request dto.AnalyzeEmailRequest) dto.AnalyzeEmailResponse {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailAnalysisService.getStructuredEmailBody")
	defer spans.Finish()
	spans.LogObjectAsJson("request", request)

	// TODO migrate from HTTP call to NATS request/response

	resp := dto.AnalyzeEmailResponse{
		EmailID: request.EmailID,
	}

	payload, err := json.Marshal(request)
	if err != nil {
		spans.TraceError(err)
		resp.ErrorMessage = err.Error()
		return resp
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.config.Url+"/internal/v1/askAIForEmail", bytes.NewBuffer(payload))
	if err != nil {
		spans.TraceError(err)
		resp.ErrorMessage = err.Error()
		return resp
	}

	req.Header.Set("X-Openline-API-KEY", s.config.ApiKey)
	req.Header.Set("X-Openline-Username", "matt@customeros.ai")
	req.Header.Set("X-Openline-Tenant", "customerosai")

	client := &http.Client{
		Timeout: 60 * time.Second,
	}
	// Execute the request
	response, err := client.Do(req)
	if err != nil {
		spans.TraceError(err)
		resp.ErrorMessage = err.Error()
		return resp
	}
	defer response.Body.Close()

	// Read response body
	body, err := io.ReadAll(response.Body)
	if err != nil {
		spans.TraceError(err)
		resp.ErrorMessage = err.Error()
		return resp
	}

	// Check status code
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		spans.TraceError(err)
		err := fmt.Errorf("request failed with status code %d: %s", response.StatusCode, string(body))
		resp.ErrorMessage = err.Error()
		return resp
	}
	// Parse the API response
	aiResp := struct {
		EmailData dto.AnalyzeEmailResponse `json:"emailData"`
	}{}

	err = json.Unmarshal(body, &aiResp)
	if err != nil {
		spans.TraceError(err)
		resp.ErrorMessage = fmt.Sprintf("failed to parse API response: %s", err.Error())
		return resp
	}
	aiResp.EmailData.EmailID = request.EmailID

	// Return the EmailData from the response
	spans.LogObjectAsJson("response", aiResp.EmailData)
	return aiResp.EmailData
}
