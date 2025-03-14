package opensrs

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"strings"
	"text/template"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
	"github.com/pkg/errors"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/config"
	"github.com/customeros/mailstack/internal/logger"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/tracing"
)

type OpenSRSResponse struct {
	Success     bool   `json:"success"`
	Error       string `json:"error,omitempty"`
	ErrorNumber int    `json:"error_number,omitempty"`
}

type openSRSService struct {
	log           logger.Logger
	openSrsConfig *config.OpenSRSConfig
	postgres      *repository.Repositories
}

func NewOpenSRSService(log logger.Logger, openSrsConfig *config.OpenSRSConfig, postgres *repository.Repositories) interfaces.OpenSrsService {
	return &openSRSService{
		log:           log,
		openSrsConfig: openSrsConfig,
		postgres:      postgres,
	}
}

func (s *openSRSService) SendEmail(ctx context.Context, request *models.EmailMessage) error {
	span, _ := opentracing.StartSpanFromContext(ctx, "OpenSrsService.Reply")
	defer span.Finish()

	// Define the SMTP server details
	smtpHost := "mail.hostedemail.com"
	smtpPort := "587"

	mailbox, err := s.postgres.TenantSettingsMailboxRepository.GetByMailbox(ctx, request.From)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	if mailbox == nil {
		err = errors.New("mailbox not found")
		tracing.TraceErr(span, err)
		return err
	}

	toEmail := []string{}
	ccEmail := []string{}
	bccEmail := []string{}

	for _, to := range request.To {
		toEmail = append(toEmail, to)
	}
	if request.Cc != nil {
		for _, cc := range request.Cc {
			ccEmail = append(ccEmail, cc)
		}
	}
	if request.Bcc != nil {
		for _, bcc := range request.Bcc {
			bccEmail = append(bccEmail, bcc)
		}
	}

	subject := request.Subject
	inReplyTo := request.ProviderInReplyTo
	references := request.ProviderReferences

	// Compose the email headers and body
	messageTemplate := `From: {{.FromEmail}}
To: {{.ToEmail}}{{if .CCEmail}}
Cc: {{.CCEmail}}{{- end}}
Subject: {{.Subject}}
Date: {{.Date}}
Message-ID: {{.MessageId}}
In-Reply-To: {{.InReplyTo}}
References: {{.References}}
MIME-Version: 1.0
Content-Type: multipart/alternative; boundary="{{.Boundary}}"

--{{.Boundary}}
Content-Type: text/plain; charset=US-ASCII; format=flowed

{{.PlainBody}}
--{{.Boundary}}
Content-Type: text/html; charset=UTF-8

{{.HTMLBody}}
--{{.Boundary}}--
`

	plainText, err := HTMLToPlainText(request.Content)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	data := struct {
		FromEmail  string
		ToEmail    string
		CCEmail    string
		BCCEmail   string
		Subject    string
		Date       string
		MessageId  string
		InReplyTo  string
		References string
		Boundary   string
		PlainBody  string
		HTMLBody   string
	}{
		ToEmail:    strings.Join(toEmail, ", "),
		CCEmail:    strings.Join(ccEmail, ", "),
		BCCEmail:   strings.Join(bccEmail, ", "),
		Subject:    subject,
		Date:       time.Now().Format("Mon, 02 Jan 2006 15:04:05 -0700"),
		MessageId:  generateMessageID(mailbox.MailboxUsername),
		InReplyTo:  inReplyTo,
		References: references,
		Boundary:   fmt.Sprintf("=_%x", time.Now().UnixNano()),
		PlainBody:  plainText,
		HTMLBody:   request.Content,
	}

	if request.FromName != "" {
		data.FromEmail = fmt.Sprintf("%s <%s>", request.FromName, request.From)
	} else {
		data.FromEmail = request.From
	}

	tmpl, err := template.New("email").Parse(messageTemplate)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	var msgBuffer bytes.Buffer
	if err := tmpl.Execute(&msgBuffer, data); err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	msg := msgBuffer.String()

	// Combine all recipients: To, CC, BCC
	recipients := []string{}
	recipients = append(recipients, toEmail...)
	recipients = append(recipients, ccEmail...)
	recipients = append(recipients, bccEmail...)

	auth := smtp.PlainAuth("", mailbox.MailboxUsername, mailbox.MailboxPassword, smtpHost)

	// Send the email
	err = smtp.SendMail(
		fmt.Sprintf("%s:%s", smtpHost, smtpPort),
		auth,
		request.From,
		recipients,
		[]byte(msg),
	)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	request.ProviderMessageId = data.MessageId
	request.ProviderThreadId = data.MessageId
	request.ProviderInReplyTo = data.InReplyTo
	request.ProviderReferences = data.References

	return nil
}

func HTMLToPlainText(html string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", err
	}

	// Remove script and style elements
	doc.Find("script, style").Each(func(i int, el *goquery.Selection) {
		el.Remove()
	})

	// Get text content
	text := doc.Find("body").Text()

	// Trim spaces and replace multiple newlines with a single one
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "\n\n", "\n")

	return text, nil
}

func generateMessageID(fromEmail string) string {
	// Extract the mailbox part of the email address
	mailbox := fromEmail[:strings.IndexByte(fromEmail, '@')]

	// Generate a unique identifier using the mailbox and current timestamp
	uniqueID := fmt.Sprintf("%x", md5.Sum([]byte(fmt.Sprintf("%s.%d", mailbox, time.Now().UnixNano()))))

	// Construct the final Message-ID
	domain := fromEmail[strings.IndexByte(fromEmail, '@')+1:]
	messageID := fmt.Sprintf("<%s@%s>", uniqueID, domain)

	return messageID
}

func (s *openSRSService) SetupDomain(ctx context.Context, tenant, domain string) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "OpensrsService.SetupDomain")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)
	tracing.TagTenant(span, tenant)
	span.LogKV("domain", domain)

	// step 1: get domain record from the database
	domainRecord, err := s.postgres.DomainRepository.GetDomain(ctx, tenant, domain)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "failed to get domain record"))
		s.log.Error("failed to get domain record", err)
		return err
	}
	if domainRecord == nil {
		tracing.TraceErr(span, errors.New("domain record not found"))
		s.log.Errorf("domain record not found for domain")
		return errors.New("domain record not found")
	}

	// step 2: Configure the domain in OpenSRS
	err = s.setEmailDomainInOpenSRS(ctx, domain, domainRecord.DkimPrivate)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "failed to configure email domain in open SRS"))
		s.log.Error("failed to configure email domain in open SRS", err)
		return err
	}

	return nil
}

func (s *openSRSService) setEmailDomainInOpenSRS(ctx context.Context, domain, dkimPrivateKey string) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "OpensrsService.setEmailDomainInOpenSRS")
	defer span.Finish()
	span.LogKV("domain", domain)

	// validate if open srs is configured
	if s.openSrsConfig.Username == "" || s.openSrsConfig.ApiKey == "" {
		tracing.TraceErr(span, errors.New("OpenSRS credentials not set"))
		s.log.Error("OpenSRS credentials not set")
		return errors.New("OpenSRS credentials not set")
	}

	// Define the API endpoint (replace with your environment's URL)
	apiURL := s.openSrsConfig.Url + "/api/change_domain"

	// Prepare the request body
	requestBody := map[string]interface{}{
		"credentials": map[string]string{
			"user":     s.openSrsConfig.Username,
			"password": s.openSrsConfig.ApiKey,
		},
		"domain": domain,
		"attributes": map[string]interface{}{
			"dkim_selector": "dkim",
			"dkim_key":      dkimPrivateKey,
		},
	}

	// Convert the request body to JSON
	requestData, err := json.Marshal(requestBody)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "failed to marshal request body"))
		s.log.Error("failed to marshal request body", err)
		return fmt.Errorf("failed to marshal request body: %v", err)
	}

	// Create a new HTTP request with context
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(requestData))
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "failed to create HTTP request"))
		s.log.Error("failed to create HTTP request", err)
		return fmt.Errorf("failed to create HTTP request: %v", err)
	}

	// Set necessary headers
	req.Header.Set("Content-Type", "application/json")

	// Create an HTTP client with a timeout
	client := &http.Client{Timeout: 10 * time.Second}

	// Make the HTTP request
	resp, err := client.Do(req)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "failed to make API request"))
		s.log.Error("failed to make API request", err)
		return fmt.Errorf("failed to make API request: %s", err.Error())
	}
	defer resp.Body.Close()

	// Parse the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "failed to read response body"))
		s.log.Error("failed to read response body", err)
		return errors.Wrap(err, "failed to read response body")
	}
	span.LogKV("responseBody", string(body))

	// Check for a successful response
	var response OpenSRSResponse
	err = json.Unmarshal(body, &response)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "failed to unmarshal response"))
		s.log.Error("failed to unmarshal response", err)
		return fmt.Errorf("failed to unmarshal response: %v", err)
	}

	// Check if the response indicates success
	if !response.Success {
		tracing.TraceErr(span, errors.New(response.Error))
		s.log.Error("API request failed", response.Error)
		return fmt.Errorf("API request failed: %s", response.Error)
	}

	return nil
}

func (s *openSRSService) SetupMailbox(ctx context.Context, tenant, username, password string, forwardingTo []string, webmailEnabled bool) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "OpensrsService.SetupMailbox")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)
	tracing.TagTenant(span, tenant)
	span.LogKV("username", username)
	span.LogFields(log.Bool("webmailEnabled", webmailEnabled), log.Object("forwardingTo", forwardingTo))

	// validate if open srs is configured
	if s.openSrsConfig.Username == "" || s.openSrsConfig.ApiKey == "" {
		tracing.TraceErr(span, errors.New("OpenSRS credentials not set"))
		s.log.Error("OpenSRS credentials not set")
		return errors.New("OpenSRS credentials not set")
	}

	// Define the API endpoint for adding a mailbox (replace with your environment's URL)
	apiURL := s.openSrsConfig.Url + "/api/change_user"

	if s.openSrsConfig.Username == "" || s.openSrsConfig.ApiKey == "" {
		tracing.TraceErr(span, errors.New("OpenSRS credentials not set"))
		s.log.Error("OpenSRS credentials not set")
		return errors.New("OpenSRS credentials not set")
	}

	// prepare the attributes for the openSRS API
	attributes := map[string]interface{}{
		"type":           "mailbox",
		"password":       password,
		"delivery_local": true, // Store mail locally
	}

	if webmailEnabled {
		attributes["service_webmail"] = "enabled"
	} else {
		attributes["service_webmail"] = "disabled"
	}
	// Add forwarding options if enabled
	if len(forwardingTo) > 0 {
		attributes["delivery_forward"] = true
		attributes["forward_recipients"] = forwardingTo
	}

	// Create the requestBody with the extracted attributes
	requestBody := map[string]interface{}{
		"credentials": map[string]string{
			"user":     s.openSrsConfig.Username,
			"password": s.openSrsConfig.ApiKey,
		},
		"user":       username,
		"attributes": attributes,
	}

	// Convert the request body to JSON
	requestData, err := json.Marshal(requestBody)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "failed to marshal request body"))
		s.log.Error("failed to marshal request body", err)
		return fmt.Errorf("failed to marshal request body: %v", err)
	}

	// Create a new HTTP request with context
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(requestData))
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "failed to create HTTP request"))
		s.log.Error("failed to create HTTP request", err)
		return fmt.Errorf("failed to create HTTP request: %s", err.Error())
	}

	// Set necessary headers
	req.Header.Set("Content-Type", "application/json")

	// Create an HTTP client with a timeout
	client := &http.Client{Timeout: 10 * time.Second}

	// Make the HTTP request
	resp, err := client.Do(req)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "failed to make API request"))
		s.log.Error("failed to make API request", err)
		return fmt.Errorf("failed to make API request: %s", err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		tracing.TraceErr(span, errors.New("API request failed"))
		s.log.Error("API request failed", err)
		return fmt.Errorf("API request failed, status code: %d", resp.StatusCode)
	}

	// Parse the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "failed to read response body"))
		s.log.Error("failed to read response body", err)
		return err
	}
	span.LogKV("OpenSRS.responseBody", string(body))

	// Check for a successful response
	var response OpenSRSResponse
	err = json.Unmarshal(body, &response)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "failed to unmarshal response"))
		s.log.Error("failed to unmarshal response", err)
		return err
	}

	// Check if the response indicates success
	if !response.Success {
		tracing.TraceErr(span, errors.New(response.Error))
		s.log.Error("API request failed", response.Error)
		return err
	}

	return nil
}

func (s *openSRSService) GetMailboxDetails(ctx context.Context, email string) (interfaces.MailboxDetails, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "OpensrsService.GetMailboxDetails")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)
	span.LogKV("email", email)

	// validate if open srs config
	if s.openSrsConfig.Username == "" || s.openSrsConfig.ApiKey == "" {
		tracing.TraceErr(span, errors.New("OpenSRS credentials not set"))
		s.log.Error("OpenSRS credentials not set")
		return interfaces.MailboxDetails{}, errors.New("OpenSRS credentials not set")
	}

	// Define the API endpoint for getting mailbox information
	apiURL := s.openSrsConfig.Url + "/api/get_user"

	// Create the request body
	requestBody := map[string]interface{}{
		"credentials": map[string]string{
			"user":     s.openSrsConfig.Username,
			"password": s.openSrsConfig.ApiKey,
		},
		"user": email,
	}

	// Convert the request body to JSON
	requestData, err := json.Marshal(requestBody)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "failed to marshal request body"))
		s.log.Error("failed to marshal request body", err)
		return interfaces.MailboxDetails{}, fmt.Errorf("failed to marshal request body: %s", err.Error())
	}

	// Create a new HTTP request with context
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(requestData))
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "failed to create HTTP request"))
		s.log.Error("failed to create HTTP request", err)
		return interfaces.MailboxDetails{}, fmt.Errorf("failed to create HTTP request: %s", err.Error())
	}

	// Set necessary headers
	req.Header.Set("Content-Type", "application/json")

	// Create an HTTP client with a timeout
	client := &http.Client{Timeout: 10 * time.Second}

	// Make the HTTP request
	resp, err := client.Do(req)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "failed to make API request"))
		s.log.Error("failed to make API request", err)
		return interfaces.MailboxDetails{}, fmt.Errorf("failed to make API request: %s", err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		tracing.TraceErr(span, errors.New("API request failed"))
		s.log.Error("API request failed")
		return interfaces.MailboxDetails{}, fmt.Errorf("API request failed")
	}

	// Parse the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "failed to read response body"))
		s.log.Error("failed to read response body", err)
		return interfaces.MailboxDetails{}, err
	}
	span.LogKV("responseBody", string(body))

	// Define a map to parse the response
	var response map[string]interface{}
	err = json.Unmarshal(body, &response)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "failed to unmarshal response"))
		s.log.Error("failed to unmarshal response", err)
		return interfaces.MailboxDetails{}, err
	}

	// Check if the response indicates success
	if success, ok := response["success"].(bool); !ok || !success {
		errMessage := response["error"].(string)
		tracing.TraceErr(span, errors.New(errMessage))
		s.log.Error("API request failed", errMessage)
		return interfaces.MailboxDetails{}, fmt.Errorf("API request failed: %s", errMessage)
	}

	// Extract the mailbox details: creation date and attributes
	attributes := response["attributes"].(map[string]interface{})
	mailboxDetails := interfaces.MailboxDetails{
		Email:             email,
		ForwardingEnabled: attributes["delivery_forward"].(bool),
	}
	recipients := make([]string, 0)
	for _, recipient := range attributes["forward_recipients"].([]interface{}) {
		if str, ok := recipient.(string); ok {
			recipients = append(recipients, str)
		}
	}
	mailboxDetails.ForwardingTo = recipients
	if (attributes["service_webmail"].(string)) == "enabled" {
		mailboxDetails.WebmailEnabled = true
	}

	return mailboxDetails, nil
}
