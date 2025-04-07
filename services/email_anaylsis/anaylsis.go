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
	"github.com/customeros/mailstack/proto/pb"
)

func (s *EmailAnalysisService) processRequestForStructuredBody(ctx context.Context, message *pb.AnalyzeEmailRequest) *pb.AnalyzeEmailResponse {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailAnalysisService.processRequestForStructuredBody")
	defer spans.Finish()

	// TODO migrate from HTTP call to NATS request/response

	req := dto.AskAIForEmailRequest{
		EmailFrom:        message.From.Name,
		FromEmailAddress: message.From.Email,
		ToEmailAddress:   message.To[0].Name,
		EmailBodyText:    message.EmailBodyText,
		EmailBodyHTML:    message.EmailBodyHtml,
	}

	emailBody, err := s.getStructuredBody(ctx, req)
	if err != nil {
		spans.TraceError(err)
		return emailResponseToPb(message.EmailId, nil, err.Error())
	}

	return emailResponseToPb(message.EmailId, emailBody, "")
}

func (s *EmailAnalysisService) getStructuredBody(ctx context.Context, request dto.AskAIForEmailRequest) (*dto.EmailResponse, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailAnalysisService.processRequestForStructuredBody")
	defer spans.Finish()

	payload, err := json.Marshal(request)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.config.Url+"/internal/v1/askAIForEmail", bytes.NewBuffer(payload))
	if err != nil {
		spans.TraceError(err)
		return nil, err
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
		return nil, err
	}
	defer response.Body.Close()

	// Read response body
	body, err := io.ReadAll(response.Body)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	// Check status code
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		spans.TraceError(err)
		err := fmt.Errorf("request failed with status code %d: %s", response.StatusCode, string(body))
		return nil, err
	}
	// Parse the API response
	resp := &dto.AskAIForEmailResponse{}

	err = json.Unmarshal(body, resp)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}
	// Return the EmailData from the response
	spans.LogObjectAsJson("response", resp.EmailData)
	return &resp.EmailData, nil
}

// EmailResponseToPb converts an EmailResponse to a pb.AnalyzeEmailResponse
func emailResponseToPb(emailId string, response *dto.EmailResponse, errorMessage string) *pb.AnalyzeEmailResponse {
	if response == nil {
		return &pb.AnalyzeEmailResponse{
			EmailId:      emailId,
			ErrorMessage: errorMessage,
		}
	}

	return &pb.AnalyzeEmailResponse{
		EmailId:             emailId,
		HasSignature:        response.HasSignature,
		MessageBodyMarkdown: response.MessageBody,
		Signature:           emailSignatureToPb(&response.Signature),
		ErrorMessage:        errorMessage,
	}
}

// emailSignatureToPb converts an EmailSignature to a pb.EmailSignature
func emailSignatureToPb(signature *dto.EmailSignature) *pb.EmailSignature {
	if signature == nil {
		return nil
	}

	return &pb.EmailSignature{
		CompanyInfo: emailSignatureCompanyInfoToPb(&signature.CompanyInfo),
		ContactInfo: emailSignatureContactInfoToPb(&signature.ContactInfo),
	}
}

// emailSignatureContactInfoToPb converts an EmailSignatureContactInfo to a pb.EmailSignatureContactInfo
func emailSignatureContactInfoToPb(info *dto.EmailSignatureContactInfo) *pb.EmailSignatureContactInfo {
	if info == nil {
		return nil
	}

	return &pb.EmailSignatureContactInfo{
		Name:         info.Name,
		JobTitle:     info.JobTitle,
		Company:      info.Company,
		Email:        info.Email,
		Phone:        info.Phone,
		Mobile:       info.Mobile,
		Linkedin:     info.LinkedIn,
		Github:       info.GitHub,
		CalendarLink: info.CalendarLink,
	}
}

// emailSignatureCompanyInfoToPb converts an EmailSignatureCompanyInfo to a pb.EmailSignatureCompanyInfo
func emailSignatureCompanyInfoToPb(info *dto.EmailSignatureCompanyInfo) *pb.EmailSignatureCompanyInfo {
	if info == nil {
		return nil
	}

	return &pb.EmailSignatureCompanyInfo{
		Website:   info.Website,
		Linkedin:  info.LinkedIn,
		Twitter:   info.Twitter,
		Youtube:   info.Youtube,
		Instagram: info.Instagram,
		Github:    info.GitHub,
		Address:   emailSignatureAddressToPb(&info.Address),
		// Note: domain field isn't in the original struct, so it's not set
	}
}

// emailSignatureAddressToPb converts an EmailSignatureAddress to a pb.EmailSignatureAddress
func emailSignatureAddressToPb(address *dto.EmailSignatureAddress) *pb.EmailSignatureAddress {
	if address == nil {
		return nil
	}

	return &pb.EmailSignatureAddress{
		Street:     address.Street,
		City:       address.City,
		Region:     address.Region,
		PostalCode: address.PostalCode,
		Country:    address.Country,
	}
}
