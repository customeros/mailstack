package email_content

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/customeros/mailstack/interfaces"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/customeros/mailsherpa/mailvalidate"
	"github.com/jhillyerd/enmime"
	"github.com/lib/pq"
	"github.com/nats-io/nats.go"
	"go.uber.org/multierr"
	"google.golang.org/protobuf/proto"

	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/telemetry"
	"github.com/customeros/mailstack/internal/utils"
	"github.com/customeros/mailstack/proto/helpers"
	pb_mappers "github.com/customeros/mailstack/proto/mappers"
	"github.com/customeros/mailstack/proto/pb"
)

const REQUEST_TIMEOUT = 60 * time.Second

func (s *EmailContentService) processEmail(ctx context.Context, event *pb.EmailStored) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailContentService.processEmail")
	defer spans.Finish()

	// Get eml from storage
	eml, err := s.emlStorage.Download(ctx, event.EmlKey)
	if err != nil {
		spans.TraceError(err)
		return err
	}

	// Process eml into structured envelope
	reader := bytes.NewReader(eml)
	envelope, err := enmime.ReadEnvelope(reader)
	if err != nil {
		spans.TraceError(err)
		return err
	}

	classificationReq, classificationResp, err := s.getEmailClassification(ctx, event.EmailId, event.MailboxId, envelope)
	if err != nil {
		spans.TraceError(err)
		return err
	}
	if classificationResp == nil {
		err = errors.New("Unable to classify email")
		spans.TraceError(err)
		return err
	}

	classification := pb_mappers.PbToEmailClassification(classificationResp.Classification)

	switch classification {
	case enum.EmailOK:
		err := s.processEmailContent(ctx, classificationReq, envelope, event.MailboxId)
		return err
	// case enum.EmailBounceNotification:
	// 	// TODO publish bounce notification
	// 	return nil
	// case enum.EmailAutoResponder:
	// 	// TODO publish autoresponder notification
	// 	return nil
	default:
		err := s.publishSkipNotification(ctx, &pb.SkipInboundProcessing{
			EmailId:        classificationReq.EmailId,
			MailboxId:      classificationReq.MailboxId,
			Classification: classificationResp.Classification,
			Details:        classificationResp.Details,
		})
		if err != nil {
			spans.TraceError(err)
		}

		replyToEmail := ""
		if classificationReq.ReplyTo != nil {
			replyToEmail = classificationReq.ReplyTo.Email
		}
		var fromAddress, fromName, fromUser, fromDomain string
		if classificationReq.From != nil {
			fromAddress = classificationReq.From.Email
			fromName = classificationReq.From.Name
			fromUser = classificationReq.From.User
			fromDomain = classificationReq.From.Domain
		}

		updates := map[string]interface{}{
			"subject":        classificationReq.Subject,
			"clean_subject":  utils.NormalizeSubject(classificationReq.Subject),
			"from_address":   fromAddress,
			"from_name":      fromName,
			"from_user":      fromUser,
			"from_domain":    fromDomain,
			"reply_to":       replyToEmail,
			"to_addresses":   pq.StringArray(getEmailsAsSlice(classificationReq.To)),
			"cc_addresses":   pq.StringArray(getEmailsAsSlice(classificationReq.Cc)),
			"bcc_addresses":  pq.StringArray(getEmailsAsSlice(classificationReq.Bcc)),
			"classification": classificationResp.Classification,
		}
		err = s.repositories.EmailLogRepository.UpdateEmailLog(ctx, classificationReq.EmailId, updates)
		if err != nil {
			spans.TraceError(err)
		}
		return err
	}
}

func (s *EmailContentService) processEmailContent(ctx context.Context, headers *pb.EmailClassificationRequest, envelope *enmime.Envelope, mailboxID string) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailContentService.processEmailContent")
	defer spans.Finish()

	var errs error
	// Start concurrent processing
	var wg sync.WaitGroup
	wg.Add(3)

	// Process body
	var bodyResult *pb.AnalyzeEmailResponse
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
	var attachmentResult *pb.ProcessAttachmentResponse
	var attachmentErr error
	go func() {
		defer wg.Done()
		attachmentResult, attachmentErr = s.processAttachments(ctx, headers.EmailId, headers.MailboxId, envelope)
		if attachmentErr != nil {
			spans.TraceError(attachmentErr)
			errs = multierr.Append(errs, attachmentErr)
		}
	}()

	// Attach message to thread
	var threadResult *pb.AttachToThreadResponse
	var threadErr error
	go func() {
		defer wg.Done()
		threadResult, threadErr = s.attachToThread(ctx, headers, envelope, mailboxID)
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

	replyToEmail := ""
	if headers.ReplyTo != nil {
		replyToEmail = headers.ReplyTo.Email
	}
	var fromAddress, fromName, fromUser, fromDomain string
	if headers.From != nil {
		fromAddress = headers.From.Email
		fromName = headers.From.Name
		fromUser = headers.From.User
		fromDomain = headers.From.Domain
	}

	updates := map[string]interface{}{
		"message_id":     threadResult.MessageId,
		"thread_id":      threadResult.ThreadId,
		"subject":        headers.Subject,
		"clean_subject":  utils.NormalizeSubject(headers.Subject),
		"from_address":   fromAddress,
		"from_name":      fromName,
		"from_user":      fromUser,
		"from_domain":    fromDomain,
		"reply_to":       replyToEmail,
		"to_addresses":   pq.StringArray(getEmailsAsSlice(headers.To)),
		"cc_addresses":   pq.StringArray(getEmailsAsSlice(headers.Cc)),
		"bcc_addresses":  pq.StringArray(getEmailsAsSlice(headers.Bcc)),
		"attachment_ids": pq.StringArray(attachmentResult.AttachmentIds),
		"body_markdown":  bodyResult.MessageBodyMarkdown,
		"has_attachment": attachmentResult.HasAttachment,
		"has_signature":  bodyResult.HasSignature,
		"classification": enum.EmailOK,
		"sent_at":        sentAt,
		"received_at":    receivedAt,
	}
	err = s.repositories.EmailLogRepository.UpdateEmailLog(ctx, headers.EmailId, updates)
	if err != nil {
		spans.TraceError(err)
		errs = multierr.Append(errs, err)
	}

	// publish completed message
	err = s.publishCompleted(ctx, &pb.InboundEmailProcessingCompleted{
		EmailId:        headers.EmailId,
		MailboxId:      headers.MailboxId,
		Classification: pb_mappers.EmailClassificationToPb(enum.EmailOK),
		ThreadId:       threadResult.ThreadId,
	})
	if err != nil {
		spans.TraceError(err)
		errs = multierr.Append(errs, err)
	}

	return errs
}

func (s *EmailContentService) getEmailClassification(ctx context.Context, emailID, mailboxID string, envelope *enmime.Envelope) (*pb.EmailClassificationRequest, *pb.EmailClassificationResponse, error) {
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
	request := &pb.EmailClassificationRequest{
		EmailId:            emailID,
		MailboxId:          mailboxID,
		Subject:            envelope.GetHeader("Subject"),
		From:               getFirstOrEmpty(from),
		To:                 to,
		Cc:                 cc,
		Bcc:                bcc,
		ReplyTo:            getFirstOrEmpty(replyto),
		ReturnPath:         envelope.GetHeader("Return-Path"),
		Unsubscribe:        envelope.GetHeader("Unsubscribe"),
		Precedence:         envelope.GetHeader("Precedence"),
		Sender:             envelope.GetHeader("Sender"),
		XAutoReply:         envelope.GetHeader("X-Autoreply"),
		XAutoResponse:      envelope.GetHeader("X-Autoresponse"),
		XLoop:              envelope.GetHeader("X-Loop"),
		XFailedRecipients:  envelope.GetHeader("X-Failed-Recipients"),
		ContentDescription: envelope.GetHeader("Content-Description"),
		FeedbackId:         envelope.GetHeader("Feedback-ID"),
		ForwardedFor:       envelope.GetHeader("Forwarded-for"),
		Dkim:               envelope.GetHeader("DKIM"),
		Spf:                envelope.GetHeader("spf"),
		Dmarc:              envelope.GetHeader("DMARC"),
		ListUnsubscribe:    envelope.GetHeader("List-unsubscribe"),
		AutoSubmitted:      envelope.GetHeader("Auto-submitted"),
	}

	resp, err := s.sendClassificationRequest(ctx, request)

	return request, resp, err
}

// ParseEmailAddresses extracts structured email addresses from an enmime envelope
func parseEmailAddresses(header string, envelope *enmime.Envelope) ([]*pb.EmailAddress, error) {
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
	parsed := make([]*pb.EmailAddress, 0, len(addresses))
	for _, addr := range addresses {

		verification := mailvalidate.ValidateEmailSyntax(addr.Address)
		if verification.IsValid {
			parsed = append(parsed, &pb.EmailAddress{
				Name:   addr.Name,
				Email:  verification.CleanEmail,
				User:   verification.User,
				Domain: verification.Domain,
			})
		}
	}

	return parsed, nil
}

func (s *EmailContentService) processBody(ctx context.Context, headers *pb.EmailClassificationRequest, envelope *enmime.Envelope) (*pb.AnalyzeEmailResponse, error) {
	bodySpan, ctx := telemetry.StartServiceSpan(ctx, "emailContentService.processBody")
	defer bodySpan.Finish()

	// Create request payload with email body content
	bodyRequest := &pb.AnalyzeEmailRequest{
		EmailId:        headers.EmailId,
		MailboxId:      headers.MailboxId,
		From:           headers.From,
		To:             headers.To,
		EmailBodyText:  envelope.Text,
		EmailBodyHtml:  envelope.HTML,
		Classification: pb_mappers.EmailClassificationToPb(enum.EmailOK),
	}

	return s.sendEmailAnalysisRequest(ctx, bodyRequest)
}

func (s *EmailContentService) processAttachments(ctx context.Context, emailID, mailboxID string, envelope *enmime.Envelope) (*pb.ProcessAttachmentResponse, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "process_attachments")
	defer spans.Finish()

	// return early if no attachments
	if len(envelope.Attachments) == 0 && len(envelope.Inlines) == 0 {
		return &pb.ProcessAttachmentResponse{
			EmailId:       emailID,
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
	attachmentRequest := &pb.ProcessAttachmentRequest{
		EmailId:        emailID,
		MailboxId:      mailboxID,
		Classification: pb_mappers.EmailClassificationToPb(enum.EmailOK),
		Attachments:    attachments,
	}

	return s.sendEmailAttchmentRequest(ctx, attachmentRequest)
}

func (s *EmailContentService) attachToThread(ctx context.Context, headers *pb.EmailClassificationRequest, envelope *enmime.Envelope, mailboxID string) (*pb.AttachToThreadResponse, error) {
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

	sentAt, receivedAt, err := parseEmailTimestamps(envelope)
	if err != nil {
		spans.TraceError(err)
	}

	replyToEmail := ""
	if headers.ReplyTo != nil {
		replyToEmail = headers.ReplyTo.Email
	}

	req := &pb.AttachToThreadRequest{
		EmailId:         headers.EmailId,
		MailboxId:       mailboxID,
		MessageId:       utils.NormalizeMessageID(envelope.GetHeader("Message-ID")),
		Classification:  pb_mappers.EmailClassificationToPb(enum.EmailOK),
		ReplyTo:         replyToEmail,
		References:      references,
		Subject:         headers.Subject,
		AllParticipants: getAllParticipants(headers),
		EmailSentAt:     utils.TimePointerToProto(sentAt),
		EmailReceivedAt: utils.TimePointerToProto(receivedAt),
	}

	return s.sendAttachToThreadRequest(ctx, req)
}

func getAllParticipants(headers *pb.EmailClassificationRequest) []string {
	// Use a map to track unique email addresses
	uniqueEmails := make(map[string]struct{})

	// Add the sender email
	uniqueEmails[headers.From.Email] = struct{}{}

	// Add all recipient emails
	allRecipients := helpers.AllRecipients(headers)
	for _, email := range allRecipients {
		uniqueEmails[email] = struct{}{}
	}

	// Convert the map keys back to a slice
	result := make([]string, 0, len(uniqueEmails))
	for email := range uniqueEmails {
		result = append(result, email)
	}

	return result
}

// Generic method to send requests to services
func (s *EmailContentService) sendClassificationRequest(ctx context.Context, request *pb.EmailClassificationRequest) (*pb.EmailClassificationResponse, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailContentService.sendClassificationRequest")
	defer spans.Finish()

	// Marshal request to protobuf
	reqData, err := proto.Marshal(request)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	// Send request to service
	msg := nats.NewMsg(enum.EventEmailInboundClassify.String())
	msg.Header = nats.Header{
		interfaces.HEADER_TENANT: []string{utils.GetTenantFromContext(ctx)},
		interfaces.HEADER_USERID: []string{utils.GetUserIdFromContext(ctx)},
	}
	msg.Data = reqData

	resp, err := s.natsConn.Conn.RequestMsg(msg, REQUEST_TIMEOUT/3)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	// Unmarshal response
	response := &pb.EmailClassificationResponse{}
	if err := proto.Unmarshal(resp.Data, response); err != nil {
		spans.TraceError(err)
		return nil, err
	}

	return response, nil
}

func (s *EmailContentService) sendEmailAnalysisRequest(ctx context.Context, request *pb.AnalyzeEmailRequest) (*pb.AnalyzeEmailResponse, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailContentService.sendEmailAnalysisRequest")
	defer spans.Finish()

	// Marshal request to JSON
	reqData, err := proto.Marshal(request)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	// Send request to service
	msg := nats.NewMsg(enum.EventEmailInboundAnalysis.String())
	msg.Header = nats.Header{
		interfaces.HEADER_TENANT: []string{utils.GetTenantFromContext(ctx)},
		interfaces.HEADER_USERID: []string{utils.GetUserIdFromContext(ctx)},
	}
	msg.Data = reqData

	resp, err := s.natsConn.Conn.RequestMsg(msg, REQUEST_TIMEOUT)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	// Unmarshal response
	response := &pb.AnalyzeEmailResponse{}
	if err := proto.Unmarshal(resp.Data, response); err != nil {
		spans.TraceError(err)
		return nil, err
	}

	return response, nil
}

func (s *EmailContentService) sendEmailAttchmentRequest(ctx context.Context, request *pb.ProcessAttachmentRequest) (*pb.ProcessAttachmentResponse, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailContentService.sendEmailAttachmentRequest")
	defer spans.Finish()

	// Marshal request to JSON
	reqData, err := proto.Marshal(request)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	// Send request to service
	msg := nats.NewMsg(enum.EventEmailInboundAttachments.String())
	msg.Header = nats.Header{
		interfaces.HEADER_TENANT: []string{utils.GetTenantFromContext(ctx)},
		interfaces.HEADER_USERID: []string{utils.GetUserIdFromContext(ctx)},
	}
	msg.Data = reqData

	resp, err := s.natsConn.Conn.RequestMsg(msg, REQUEST_TIMEOUT)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	// Unmarshal response
	response := &pb.ProcessAttachmentResponse{}
	if err := proto.Unmarshal(resp.Data, response); err != nil {
		spans.TraceError(err)
		return nil, err
	}

	return response, nil
}

func (s *EmailContentService) sendAttachToThreadRequest(ctx context.Context, request *pb.AttachToThreadRequest) (*pb.AttachToThreadResponse, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailContentService.sendAttachToThreadRequest")
	defer spans.Finish()

	// Marshal request to JSON
	reqData, err := proto.Marshal(request)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	// Send request to service
	msg := nats.NewMsg(enum.EventEmailInboundThread.String())
	msg.Header = nats.Header{
		interfaces.HEADER_TENANT: []string{utils.GetTenantFromContext(ctx)},
		interfaces.HEADER_USERID: []string{utils.GetUserIdFromContext(ctx)},
	}
	msg.Data = reqData

	resp, err := s.natsConn.Conn.RequestMsg(msg, REQUEST_TIMEOUT/2)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	// Unmarshal response
	response := &pb.AttachToThreadResponse{}
	if err := proto.Unmarshal(resp.Data, response); err != nil {
		spans.TraceError(err)
		return nil, err
	}

	return response, nil
}

// Helper to build attachment list
func (s *EmailContentService) buildAttachmentList(ctx context.Context, envelope *enmime.Envelope, emailID string) ([]*pb.AttachmentMetadata, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailContentService.buildAttachmentList")
	defer spans.Finish()

	attachments := make([]*pb.AttachmentMetadata, 0, len(envelope.Attachments)+len(envelope.Inlines))

	// Process regular attachments
	for _, att := range envelope.Attachments {
		attMetadata, err := s.cacheAttachment(ctx, emailID, att, false)
		if err != nil {
			spans.TraceError(err)
		}
		if attMetadata != nil {
			attachments = append(attachments, attMetadata)
		}
	}

	// Similar code for inline attachments...
	for _, att := range envelope.Inlines {
		attMetadata, err := s.cacheAttachment(ctx, emailID, att, true)
		if err != nil {
			spans.TraceError(err)
		}
		if attMetadata != nil {
			attachments = append(attachments, attMetadata)
		}
	}

	return attachments, nil
}

func (s *EmailContentService) cacheAttachment(ctx context.Context, emailID string, attachment *enmime.Part, isInline bool) (*pb.AttachmentMetadata, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailContentService.cacheAttachment")
	defer spans.Finish()

	bucketName := enum.NATSBucketEmailAttachment.String()

	// Try to get the object store first
	objStore, err := s.natsConn.JS.ObjectStore(bucketName)
	if err != nil {
		spans.TraceError(fmt.Errorf("Error accessing object store %s: %v", bucketName, err))

		// Try to create the object store explicitly
		objStore, err = s.natsConn.JS.CreateObjectStore(&nats.ObjectStoreConfig{
			Bucket:      bucketName,
			Description: "Email attachments storage",
			TTL:         24 * time.Hour,
			Replicas:    1,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create object store: %w", err)
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
	return &pb.AttachmentMetadata{
		Filename:    attachment.FileName,
		ContentType: attachment.ContentType,
		ContentId:   attachment.ContentID,
		Size:        int32(len(attachment.Content)),
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

func getEmailsAsSlice(emailAddress []*pb.EmailAddress) []string {
	if emailAddress == nil {
		return []string{}
	}
	results := make([]string, 0, len(emailAddress))
	for _, emailAddr := range emailAddress {
		if emailAddr.Email != "" && !utils.IsStringInSlice(emailAddr.Email, results) {
			results = append(results, emailAddr.Email)
		}
	}
	return results
}

func getFirstOrEmpty(addresses []*pb.EmailAddress) *pb.EmailAddress {
	if len(addresses) > 0 {
		return addresses[0]
	}
	return nil
}
