package email_content

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/customeros/mailsherpa/mailvalidate"
	"github.com/jhillyerd/enmime"
	"github.com/nats-io/nats.go"
	"go.uber.org/multierr"

	"github.com/customeros/mailstack/dto"
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/telemetry"
	"github.com/customeros/mailstack/internal/utils"
)

func (s *EmailContentService) processEmail(ctx context.Context, event dto.EmailStored, eventRecord *models.EmailEvent) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailContentService.processEmail")
	defer spans.Finish()

	// Get eml from storage
	eml, err := s.emlStorage.Download(ctx, event.EMLKey)
	if err != nil {
		spans.TraceError(err)
		eventRecord.ErrorMessage = err.Error()
		return
	}

	// Process eml into structured envelope
	reader := bytes.NewReader(eml)
	envelope, err := enmime.ReadEnvelope(reader)
	if err != nil {
		spans.TraceError(err)
		eventRecord.ErrorMessage = err.Error()
		return
	}

	classificationReq, classificationResp, err := s.getEmailClassification(ctx, event.ID, envelope)
	if err != nil {
		spans.TraceError(err)
		eventRecord.ErrorMessage = err.Error()
		return
	}
	if classificationResp == nil {
		err = errors.New("Unable to classify email")
		spans.TraceError(err)
		eventRecord.ErrorMessage = err.Error()
		return
	}
	eventRecord.Classification = classificationResp.Classification

	switch classificationResp.Classification {
	case enum.EmailOK:
		s.processEmailContent(ctx, classificationReq, envelope, eventRecord)
		if eventRecord.ErrorMessage != "" {
			return
		}
	case enum.EmailBounceNotification:
		// TODO publish bounce notification
	case enum.EmailAutoResponder:
		// TODO publish autoresponder notification
	default:
		// skip
	}

	// Mark processing as successful
	eventRecord.Success = true
	return
}

func (s *EmailContentService) processEmailContent(ctx context.Context, headers *dto.EmailClassificationRequest, envelope *enmime.Envelope, eventRecord *models.EmailEvent) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailContentService.processEmailContent")
	defer spans.Finish()

	var errs error
	// Start concurrent processing
	var wg sync.WaitGroup
	wg.Add(3)

	// Process body
	var bodyResult *dto.AnalyzeEmailResponse
	var bodyErr error
	go func() {
		defer wg.Done()
		bodyResult, bodyErr = s.processBody(ctx, headers, envelope)
		if bodyErr != nil {
			spans.TraceError(bodyErr)
			errs = multierr.Append(errs, bodyErr)
		}

		// TODO process signature and publish message to update global contacts
	}()

	// Process attachments
	var attachmentResult *dto.ProcessAttachmentResponse
	var attachmentErr error
	go func() {
		defer wg.Done()
		attachmentResult, attachmentErr = s.processAttachments(ctx, headers.EmailID, envelope)
		if attachmentErr != nil {
			spans.TraceError(attachmentErr)
			errs = multierr.Append(errs, attachmentErr)
		}
	}()

	// Attach message to thread
	var threadResult *dto.AttachToThreadResponse
	var threadErr error
	go func() {
		defer wg.Done()
		threadResult, threadErr = s.attachToThread(ctx, headers.EmailID, envelope)
		if threadErr != nil {
			spans.TraceError(threadErr)
			errs = multierr.Append(errs, threadErr)
		}
	}()

	// Wait for all processing to complete
	wg.Wait()

	// process timestamps
	sentAt, receivedAt, err := parseEmailTimestamps(envelope)
	if err != nil {
		spans.TraceError(err)
		errs = multierr.Append(errs, err)
	}

	// get current email record and append results
	updates := map[string]interface{}{
		"message_id":     threadResult.MessageID,
		"thread_id":      threadResult.ThreadID,
		"subject":        headers.Subject,
		"clean_subject":  utils.NormalizeSubject(headers.Subject),
		"from_address":   headers.From.Email,
		"from_name":      headers.From.Name,
		"from_user":      headers.From.User,
		"from_domain":    headers.From.Domain,
		"reply_to":       headers.ReplyTo.Email,
		"to_addresses":   getEmailsAsSlice(headers.To),
		"cc_addresses":   getEmailsAsSlice(headers.Cc),
		"bcc_addresses":  getEmailsAsSlice(headers.Bcc),
		"attachment_ids": attachmentResult.AttachmentIDs,
		"body_text":      envelope.Text,
		"body_markdown":  bodyResult.MessageBodyMarkdown,
		"has_attachment": attachmentResult.HasAttachment,
		"has_signature":  bodyResult.HasSignature,
		"classification": enum.EmailOK,
		"sent_at":        sentAt,
		"received_at":    receivedAt,
	}
	err = s.repositories.EmailLogRepository.UpdateEmailLog(ctx, headers.EmailID, updates)
	if err != nil {
		spans.TraceError(err)
		errs = multierr.Append(errs, err)
	}

	// update eventRecord for timescale logging
	eventRecord.MessageID = threadResult.MessageID
	eventRecord.ThreadID = threadResult.ThreadID

	// publish completed message
	err = s.publishCompleted(ctx, &dto.InboundEmailProcessingCompleted{
		EmailID: headers.EmailID,
	})
	if err != nil {
		spans.TraceError(err)
		errs = multierr.Append(errs, err)
	}

	return errs
}

func (s *EmailContentService) getEmailClassification(ctx context.Context, emailID string, envelope *enmime.Envelope) (*dto.EmailClassificationRequest, *dto.EmailClassificationResponse, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "process_headers")
	defer spans.Finish()

	from, err := parseEmailAddresses("From", envelope)
	if err != nil {
		spans.TraceError(err)
	}
	to, err := parseEmailAddresses("To", envelope)
	if err != nil {
		spans.TraceError(err)
	}
	cc, err := parseEmailAddresses("Cc", envelope)
	if err != nil {
		spans.TraceError(err)
	}
	bcc, err := parseEmailAddresses("Bcc", envelope)
	if err != nil {
		spans.TraceError(err)
	}
	replyto, err := parseEmailAddresses("Reply-to", envelope)
	if err != nil {
		spans.TraceError(err)
	}

	// Create request payload with email headers
	request := dto.EmailClassificationRequest{
		EmailID:            emailID,
		Subject:            envelope.GetHeader("Subject"),
		From:               from[0],
		To:                 to,
		Cc:                 cc,
		Bcc:                bcc,
		ReplyTo:            replyto[0],
		ReturnPath:         envelope.GetHeader("Return-Path"),
		Unsubscribe:        envelope.GetHeader("Unsubscribe"),
		Precedence:         envelope.GetHeader("Precedence"),
		Sender:             envelope.GetHeader("Sender"),
		XAutoReply:         envelope.GetHeader("X-Autoreply"),
		XAutoResponse:      envelope.GetHeader("X-Autoresponse"),
		XLoop:              envelope.GetHeader("X-Loop"),
		XFailedRecipients:  envelope.GetHeader("X-Failed-Recipients"),
		ContentDescription: envelope.GetHeader("Content-Description"),
		FeedbackID:         envelope.GetHeader("Feedback-ID"),
		ForwardedFor:       envelope.GetHeader("Forwarded-for"),
		DKIM:               envelope.GetHeader("DKIM"),
		SPF:                envelope.GetHeader("spf"),
		DMARC:              envelope.GetHeader("DMARC"),
		ListUnsubscribe:    envelope.GetHeader("List-unsubscribe"),
		AutoSubmitted:      envelope.GetHeader("Auto-submitted"),
	}

	resp, err := s.sendClassificationRequest(ctx, request)

	return &request, resp, err
}

// ParseEmailAddresses extracts structured email addresses from an enmime envelope
func parseEmailAddresses(header string, envelope *enmime.Envelope) ([]dto.EmailAddress, error) {
	value := envelope.GetHeader(header)
	if value == "" {
		return nil, nil
	}

	// Parse the addresses
	addresses, err := mail.ParseAddressList(value)
	if err != nil {
		return nil, fmt.Errorf("error parsing %s header: %w", header, err)
	}

	// Convert to our EmailAddress struct
	parsed := make([]dto.EmailAddress, 0, len(addresses))
	for _, addr := range addresses {

		verification := mailvalidate.ValidateEmailSyntax(addr.Address)
		if verification.IsValid {
			parsed = append(parsed, dto.EmailAddress{
				Name:   addr.Name,
				Email:  verification.CleanEmail,
				User:   verification.User,
				Domain: verification.Domain,
			})
		}
	}

	return parsed, nil
}

func (s *EmailContentService) processBody(ctx context.Context, headers *dto.EmailClassificationRequest, envelope *enmime.Envelope) (*dto.AnalyzeEmailResponse, error) {
	bodySpan, ctx := telemetry.StartServiceSpan(ctx, "emailContentService.processBody")
	defer bodySpan.Finish()

	// Create request payload with email body content
	bodyRequest := dto.AnalyzeEmailRequest{
		EmailID:       headers.EmailID,
		From:          headers.From,
		To:            headers.To,
		EmailBodyText: envelope.Text,
		EmailBodyHTML: envelope.HTML,
	}

	return s.sendEmailAnalysisRequest(ctx, bodyRequest)
}

func (s *EmailContentService) processAttachments(ctx context.Context, emailID string, envelope *enmime.Envelope) (*dto.ProcessAttachmentResponse, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "process_attachments")
	defer spans.Finish()

	// return early if no attachments
	if len(envelope.Attachments) == 0 && len(envelope.Inlines) == 0 {
		return &dto.ProcessAttachmentResponse{
			EmailID:       emailID,
			HasAttachment: false,
		}, nil
	}

	// Create attachment metadata list
	attachments, err := s.buildAttachmentList(ctx, envelope, emailID)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	// Create request payload
	attachmentRequest := dto.ProcessAttachmentRequest{
		EmailID:     emailID,
		Attachments: attachments,
	}

	return s.sendEmailAttchmentRequest(ctx, attachmentRequest)
}

func (s *EmailContentService) attachToThread(ctx context.Context, emailID string, envelope *enmime.Envelope) (*dto.AttachToThreadResponse, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "process_attachments")
	defer spans.Finish()

	var references []string
	refHeader := envelope.GetHeader("References")
	if refHeader != "" {
		referenceIDs := strings.Fields(refHeader)
		for _, refID := range referenceIDs {
			references = append(references, utils.NormalizeMessageID(refID))
		}
	}

	req := dto.AttachToThreadRequest{
		EmailID:    emailID,
		MessageID:  utils.NormalizeMessageID(envelope.GetHeader("Message-ID")),
		InReplyTo:  utils.NormalizeMessageID(envelope.GetHeader("In-Reply-To")),
		References: references,
	}

	return s.sendAttachToThreadRequest(ctx, req)
}

// Generic method to send requests to services
func (s *EmailContentService) sendClassificationRequest(ctx context.Context, request dto.EmailClassificationRequest) (*dto.EmailClassificationResponse, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailContentService.sendClassificationRequest")
	defer spans.Finish()

	// Marshal request to JSON
	reqData, err := json.Marshal(request)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	// Send request to service
	msg, err := s.natsConn.Conn.Request(enum.EventEmailInboundClassify.String(), reqData, 10*time.Second)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	// Unmarshal response
	var response dto.EmailClassificationResponse
	if err := json.Unmarshal(msg.Data, &response); err != nil {
		spans.TraceError(err)
		return nil, err
	}

	return &response, nil
}

func (s *EmailContentService) sendEmailAnalysisRequest(ctx context.Context, request dto.AnalyzeEmailRequest) (*dto.AnalyzeEmailResponse, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailContentService.sendEmailAnalysisRequest")
	defer spans.Finish()

	// Marshal request to JSON
	reqData, err := json.Marshal(request)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	// Send request to service
	msg, err := s.natsConn.Conn.Request(enum.EventEmailInboundAnalysis.String(), reqData, 60*time.Second)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	// Unmarshal response
	var response dto.AnalyzeEmailResponse
	if err := json.Unmarshal(msg.Data, &response); err != nil {
		spans.TraceError(err)
		return nil, err
	}

	return &response, nil
}

func (s *EmailContentService) sendEmailAttchmentRequest(ctx context.Context, request dto.ProcessAttachmentRequest) (*dto.ProcessAttachmentResponse, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailContentService.sendEmailAttachmentRequest")
	defer spans.Finish()

	// Marshal request to JSON
	reqData, err := json.Marshal(request)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	// Send request to service
	msg, err := s.natsConn.Conn.Request(enum.EventEmailInboundAttachments.String(), reqData, 60*time.Second)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	// Unmarshal response
	var response dto.ProcessAttachmentResponse
	if err := json.Unmarshal(msg.Data, &response); err != nil {
		spans.TraceError(err)
		return nil, err
	}

	return &response, nil
}

func (s *EmailContentService) sendAttachToThreadRequest(ctx context.Context, request dto.AttachToThreadRequest) (*dto.AttachToThreadResponse, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailContentService.sendAttachToThreadRequest")
	defer spans.Finish()

	// Marshal request to JSON
	reqData, err := json.Marshal(request)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	// Send request to service
	msg, err := s.natsConn.Conn.Request(enum.EventEmailInboundThread.String(), reqData, 60*time.Second)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	// Unmarshal response
	var response dto.AttachToThreadResponse
	if err := json.Unmarshal(msg.Data, &response); err != nil {
		spans.TraceError(err)
		return nil, err
	}

	return &response, nil
}

// Helper to build attachment list
func (s *EmailContentService) buildAttachmentList(ctx context.Context, envelope *enmime.Envelope, emailID string) ([]dto.AttachmentMetadata, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailContentService.buildAttachmentList")
	defer spans.Finish()

	attachments := make([]dto.AttachmentMetadata, 0, len(envelope.Attachments)+len(envelope.Inlines))

	// Process regular attachments
	for _, att := range envelope.Attachments {
		attMetadata, err := s.cacheAttachment(ctx, emailID, att, false)
		if err != nil {
			spans.TraceError(err)
		}
		if attMetadata != nil {
			attachments = append(attachments, *attMetadata)
		}
	}

	// Similar code for inline attachments...
	for _, att := range envelope.Inlines {
		attMetadata, err := s.cacheAttachment(ctx, emailID, att, true)
		if err != nil {
			spans.TraceError(err)
		}
		if attMetadata != nil {
			attachments = append(attachments, *attMetadata)
		}
	}

	return attachments, nil
}

func (s *EmailContentService) cacheAttachment(ctx context.Context, emailID string, attachment *enmime.Part, isInline bool) (*dto.AttachmentMetadata, error) {
	// Get or create object store (bucket)
	objStore, err := s.natsConn.JS.ObjectStore("EMAIL_ATTACHMENTS")
	if err != nil {
		// If bucket doesn't exist, create it
		if err == nats.ErrBucketNotFound {
			objStore, err = s.natsConn.JS.CreateObjectStore(&nats.ObjectStoreConfig{
				Bucket:      "EMAIL_ATTACHMENTS",
				Description: "Email attachments storage",
			})
			if err != nil {
				return nil, fmt.Errorf("failed to create object store: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to access object store: %w", err)
		}
	}

	// Generate unique key for this attachment
	objectName := fmt.Sprintf("%s/%s", emailID, utils.SanitizeFilename(attachment.FileName))

	// Create a full ObjectMeta with all the metadata
	meta := &nats.ObjectMeta{
		Name:        objectName,
		Description: fmt.Sprintf("Attachment for email %s", emailID),
		Metadata: map[string]string{
			"Content-Type":      attachment.ContentType,
			"Content-ID":        attachment.ContentID,
			"Original-Filename": attachment.FileName,
		},
	}

	// Store attachment in NATS object store using Put with metadata
	info, err := objStore.Put(meta, bytes.NewReader(attachment.Content))
	if err != nil {
		return nil, fmt.Errorf("failed to store attachment: %w", err)
	}

	// Add metadata without the content
	return &dto.AttachmentMetadata{
		Filename:    attachment.FileName,
		ContentType: attachment.ContentType,
		ContentID:   attachment.ContentID,
		Size:        len(attachment.Content),
		IsInline:    isInline,
		StorageKey:  objectName,
		ObjectInfo:  info.Name,
	}, nil
}

// parseEmailTimestamps extracts and parses the sent and received timestamps from an email
func parseEmailTimestamps(envelope *enmime.Envelope) (sentAt, receivedAt *time.Time, err error) {
	// Parse the Date header for sentAt
	dateHeader := envelope.GetHeader("Date")
	if dateHeader != "" {
		// The mail.ParseDate function handles the RFC822/RFC1123 format used in emails
		parsedSentAt, err := mail.ParseDate(dateHeader)
		if err == nil {
			sentAt = &parsedSentAt
		}
	}

	// Parse the Received header for receivedAt
	// Note: There can be multiple Received headers, we'll take the first one
	// which is typically the most recent (closest to the recipient)
	receivedHeader := envelope.GetHeader("Received")
	if receivedHeader != "" {
		// Extract the date part from the Received header
		// Format is typically: "from X by Y for Z; Tue, 29 Mar 2022 14:05:23 +0000"
		dateParts := strings.Split(receivedHeader, ";")
		if len(dateParts) > 1 {
			dateStr := strings.TrimSpace(dateParts[len(dateParts)-1])
			parsedReceivedAt, err := mail.ParseDate(dateStr)
			if err == nil {
				receivedAt = &parsedReceivedAt
			}
		}
	}

	return sentAt, receivedAt, nil
}

func getEmailsAsSlice(emailAddress []dto.EmailAddress) []string {
	results := make([]string, 0, len(emailAddress))
	for _, emailAddr := range emailAddress {
		if emailAddr.Email != "" {
			results = append(results, emailAddr.Email)
		}
	}
	return results
}
