package email_processor

import (
	"context"
	"fmt"
	"strings"

	"github.com/customeros/mailsherpa/domaincheck"
	"github.com/customeros/mailsherpa/mailvalidate"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"

	"github.com/customeros/mailstack/dto"
	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/dbmapper"
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"
	"github.com/customeros/mailstack/services/events"
)

type emailProcessor struct {
	repositories  *repository.Repositories
	eventsService *events.EventsService
	aiService     interfaces.AIService
}

func NewEmailProcessor(
	repositories *repository.Repositories,
	eventsService *events.EventsService,
	aiService interfaces.AIService,
) interfaces.EmailProcessor {
	return &emailProcessor{
		repositories:  repositories,
		eventsService: eventsService,
		aiService:     aiService,
	}
}

func (p *emailProcessor) NewInboundEmail() *models.EmailStore {
	return &models.EmailStore{
		ID:         utils.GenerateNanoIDWithPrefix("email", 21),
		Direction:  enum.EmailDirectionInbound.String(),
		Status:     enum.EmailStatusReceived.String(),
		ReceivedAt: utils.NowPtr(),
	}
}

func (p *emailProcessor) NewAttachment() *models.EmailAttachment {
	return &models.EmailAttachment{
		ID:             utils.GenerateNanoIDWithPrefix("file", 12),
		StorageService: "R2",
		StorageBucket:  "attachments",
	}
}

func (p *emailProcessor) NewAttachmentFile(attachmentID string, data []byte) *interfaces.AttachmentFile {
	return &interfaces.AttachmentFile{
		ID:   attachmentID,
		Data: data,
	}
}

func (p *emailProcessor) ProcessEmail(
	ctx context.Context, email *models.EmailStore, attachments []*models.EmailAttachment, files []*interfaces.AttachmentFile,
) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailProcessor.ProcessEmail")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	// attach message to thread
	err := p.attachEmailToThread(ctx, email)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	// Clean message body
	err = p.getStructuredMessageBody(ctx, email)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	// Save the email entity to the database
	emailID, err := p.repositories.EmailRepository.Create(ctx, dbmapper.MapEmailStoreToEmail(email))
	if err != nil {
		err = errors.Wrap(err, "Error saving email")
		return err
	}
	email.ID = emailID

	// Throw events
	err = p.eventsService.Publisher.PublishFanoutEvent(ctx, emailID, enum.EMAIL, dto.EmailParticipants{Emails: email.AllParticipants()})

	return nil
}

func (p *emailProcessor) getStructuredMessageBody(ctx context.Context, email *models.EmailStore) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailProcessor.getStructuredMessageBody")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	structuredData, err := p.aiService.GetStructuredEmailBody(ctx, dto.StructuredEmailRequest{
		FromName:         email.FromName,
		FromEmailAddress: email.FromAddress,
		ToEmailAddress:   email.ToAddresses[0],
		EmailBodyText:    email.BodyText,
		EmailBodyHTML:    email.BodyHTML,
	})
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	if structuredData == nil {
		return nil
	}

	email.HasSignature = structuredData.EmailData.HasSignature
	email.BodyMarkdown = structuredData.EmailData.MessageBody

	if !structuredData.EmailData.HasSignature {
		return nil
	}

	structuredData.EmailData.Signature.CompanyInfo.Domain = email.FromDomain
	err = p.eventsService.Publisher.PublishFanoutEvent(ctx, email.ID, enum.EMAIL_SIGNATURE, structuredData.EmailData.Signature)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	return nil
}

func (p *emailProcessor) EmailFilter(ctx context.Context, email *models.EmailStore, rawHeaders map[string]interface{}) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailFilterService.ScanEmail")
	tracing.SetDefaultServiceSpanTags(ctx, span)
	defer span.Finish()

	headers, err := processHeaders(rawHeaders)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}
	if headers == nil {
		err := errors.New("email headers are nil")
		tracing.TraceErr(span, err)
		return err
	}

	isBounceNotification, reason := isBounceNotification(headers, email.Subject, email.FromAddress)
	if isBounceNotification {
		// todo determine what email bounced
		// todo send bounced email event
		email.Classification = enum.EmailBounceNotification.String()
		email.ClassificationReason = reason
		return nil
	}

	isAutoresponder, reason := isAutoresponder(headers)
	if isAutoresponder {
		// todo analyze autoresponder content and do something
		email.Classification = enum.EmailAutoResponder.String()
		email.ClassificationReason = reason
		return nil
	}

	isBulkEmail, reason := isBulkEmail(headers, email.ReplyTo, email.FromAddress)
	if isBulkEmail {
		email.Classification = enum.EmailBulk.String()
		email.ClassificationReason = reason
		return nil
	}

	isInternal := isInternalEmail(email)
	if isInternal {
		email.Classification = enum.EmailInternal.String()
		return nil
	}

	isSensitive, reason := isSensitiveSubject(email.Subject)
	if isSensitive {
		email.Classification = enum.EmailSensitive.String()
		email.ClassificationReason = reason
		return nil
	}

	// todo add spam check + email warmer check (if required)

	email.Classification = enum.EmailOK.String()
	return nil
}

func isSensitiveSubject(subject string) (bool, string) {
	// Convert subject to lowercase for case-insensitive matching
	lowerSubject := strings.ToLower(subject)

	// Define keyword groups with associated reasons
	sensitiveKeywords := map[string][]string{
		"confidentiality": {
			"confidential", "private", "sensitive", "do not share", "do not forward",
			"nda", "under nda", "confidentiality agreement", "privileged",
			"secret", "restricted", "internal only", "internal use", "not for distribution",
		},
		"financial": {
			"financial report", "quarterly results", "annual results", "revenue",
			"profit margin", "earnings", "balance sheet", "tax", "invoice", "salary",
			"compensation", "bonus", "stock options", "equity",
		},
		"legal": {
			"legal", "lawsuit", "litigation", "settlement", "contract review",
			"agreement", "terms", "legal review", "compliance", "regulatory",
			"attorney", "counsel", "court", "subpoena", "trademark",
		},
		"personal": {
			"personal", "medical", "health", "patient", "ssn", "social security",
			"date of birth", "dob", "passport", "driver license", "id number",
			"background check", "performance review",
		},
		"security": {
			"password", "login", "credentials", "access code", "security", "breach",
			"vulnerability", "hack", "incident", "authentication",
		},
		"merger": {
			"merger", "acquisition", "m&a", "due diligence", "deal", "takeover",
			"buyout", "transaction", "valuation", "term sheet", "loi", "letter of intent",
		},
		"hr": {
			"termination", "firing", "layoff", "severance", "redundancy",
			"disciplinary", "complaint", "grievance", "harassment", "discrimination",
			"interview", "candidate", "recruitment", "hiring",
		},
	}

	// Check each keyword group
	for category, keywords := range sensitiveKeywords {
		for _, keyword := range keywords {
			if strings.Contains(lowerSubject, keyword) {
				reason := fmt.Sprintf("Subject contains %s-related sensitive keyword: '%s'", category, keyword)
				return true, reason
			}
		}
	}

	// Check for explicit confidentiality markings
	confidentialityMarkers := []string{
		"[confidential]", "(confidential)", "***confidential***", "###confidential###",
		"[sensitive]", "(sensitive)", "***sensitive***", "###sensitive###",
		"[private]", "(private)", "***private***", "###private###",
	}

	for _, marker := range confidentialityMarkers {
		if strings.Contains(lowerSubject, marker) {
			reason := fmt.Sprintf("Subject contains explicit confidentiality marker: '%s'", marker)
			return true, reason
		}
	}

	// Check for classification levels
	classificationLevels := []string{
		"top secret", "secret", "confidential", "restricted", "classified",
		"sensitive but unclassified", "sbu", "for official use only", "fouo",
		"controlled unclassified", "cui",
	}

	for _, level := range classificationLevels {
		if strings.Contains(lowerSubject, level) {
			reason := fmt.Sprintf("Subject contains formal classification level: '%s'", level)
			return true, reason
		}
	}

	return false, ""
}

func isInternalEmail(email *models.EmailStore) bool {
	senderValidation := mailvalidate.ValidateEmailSyntax(email.FromAddress)
	if !senderValidation.IsValid || senderValidation.IsFreeAccount || senderValidation.Domain == "" {
		return false
	}

	var allRecipients []string
	allRecipients = append(allRecipients, email.ToAddresses...)
	allRecipients = append(allRecipients, email.CcAddresses...)
	allRecipients = append(allRecipients, email.BccAddresses...)

	if len(allRecipients) == 0 {
		return false
	}

	for _, recipient := range allRecipients {
		recipientValidation := mailvalidate.ValidateEmailSyntax(recipient)

		// Skip empty domains (malformed addresses)
		if recipientValidation.Domain == "" {
			continue
		}

		// If any domain doesn't match, the email is not internal
		if recipientValidation.Domain != senderValidation.Domain {
			return false
		}
	}

	return true
}

func isBulkEmail(headers *EmailHeaders, replyTo, from string) (bool, string) {
	matchReplyTo := false
	if replyTo == from {
		matchReplyTo = true
	}

	if headers.ForwardedFor == "" {
		switch {
		case (headers.ReplyToExists && !matchReplyTo):
			return true, "REPLY-TO != FROM"
		case headers.ReturnPathExists && headers.ReturnPath == "":
			return true, "RETURN-PATH header is empty"
		case headers.ReturnPathExists && strings.Index(headers.ReturnPath, from) == -1:
			sendingDomain := utils.ExtractDomainFromEmail(from)
			returnPathDomain := utils.ExtractDomainFromEmail(headers.ReturnPath)
			if sendingDomain != returnPathDomain {
				return true, "RETURN-PATH != FROM"
			}
		default:
		}
	}

	switch {
	case headers.ListUnsubscribe:
		return true, "UNSUBSCRIBE header present"
	case strings.EqualFold(headers.Precedence, "bulk"):
		return true, "PRECEDENCE: BULK header present"
	case headers.Sender != "" && headers.Sender != from:
		return true, "SENDER != FROM"
	default:
		return mailsherpaChecks(from)
	}
}

func mailsherpaChecks(from string) (failedCheck bool, reason string) {
	if from == "" {
		return true, "FROM is empty"
	}
	syntaxValidation := mailvalidate.ValidateEmailSyntax(from)
	if syntaxValidation.IsRoleAccount {
		return true, "FROM is a role account"
	}

	if syntaxValidation.IsSystemGenerated {
		return true, "FROM is system generated"
	}

	isPrimaryDomain, _ := domaincheck.PrimaryDomainCheck(syntaxValidation.Domain)

	if !isPrimaryDomain && !syntaxValidation.IsRoleAccount {
		return true, "Email sent from non-primary domain"
	}

	return false, ""
}

func isAutoresponder(headers *EmailHeaders) (bool, string) {
	switch {
	case headers.XAutoreply != "":
		return true, "X-AUTOREPLY header present"
	case headers.XAutoresponse != "":
		return true, "X-AUTORESPONSE header present"
	case headers.XLoop:
		return true, "X-LOOP header present"
	case strings.EqualFold(headers.Precedence, "auto_reply"):
		return true, "PRECEDENCE: AUTO_REPLY, header present"
	default:
		return false, ""
	}
}

func isBounceNotification(headers *EmailHeaders, subject, from string) (bool, string) {
	switch {
	case len(headers.XFailedRecipients) > 0:
		return true, "X-FAILED-RECIPIENTS header present"
	case strings.EqualFold(headers.ContentDescription, "delivery report"):
		return true, "CONTENT-DESCRIPTION: DELIVERY REPORT header present"
	case hasBounceKeywords(headers.ReturnPath):
		return true, "RETURN-PATH contains bounce keywords"
	case hasBounceKeywords(from):
		return true, "FROM contains bounce keywords"
	case isBounceSubject(subject):
		return true, "SUBJECT contains bounce keywords"
	default:
		return false, ""
	}
}

func hasBounceKeywords(str string) bool {
	return strings.Contains(strings.ToLower(str), "mailer-daemon")
}

func isBounceSubject(subject string) bool {
	subject = strings.ToLower(subject)
	keywords := []string{
		"mail delivery failure",
		"undelivered mail returned to sender",
		"delivery status notification",
		"undeliverable",
		"undelivered",
		"delivery failure",
		"failure notice",
		"returned mail",
		"returned to sender",
	}
	for _, phrase := range keywords {
		if strings.Contains(strings.ToLower(subject), phrase) {
			return true
		}
	}

	return false
}

type EmailHeaders struct {
	AutoSubmitted      bool
	ContentDescription string
	DeliveryStatus     bool
	ListUnsubscribe    bool
	Precedence         string
	ReturnPath         string
	ReturnPathExists   bool
	XAutoreply         string
	XAutoresponse      string
	XLoop              bool
	XFailedRecipients  []string
	ReplyTo            string
	ReplyToExists      bool
	Sender             string
	ForwardedFor       string
	DKIM               []string
	SPF                string
	DMARC              string
}

func processHeaders(rawHeaders map[string]interface{}) (*EmailHeaders, error) {
	headers := &EmailHeaders{}

	if rawHeaders == nil {
		return headers, nil
	}

	// Helper function to get header value as string
	getString := func(key string) string {
		if values, ok := rawHeaders[key].([]string); ok && len(values) > 0 {
			return values[0]
		}
		if value, ok := rawHeaders[key].(string); ok {
			return value
		}
		return ""
	}

	// Helper function to check if a header exists
	headerExists := func(key string) bool {
		_, exists := rawHeaders[key]
		return exists
	}

	// Helper to get string array
	getStringArray := func(key string) []string {
		if values, ok := rawHeaders[key].([]string); ok {
			return values
		}
		if value, ok := rawHeaders[key].(string); ok {
			return []string{value}
		}
		return nil
	}

	// Process boolean headers (presence/absence or specific values)
	autoSubmitted := getString("Auto-Submitted")
	headers.AutoSubmitted = autoSubmitted != "" && autoSubmitted != "no"

	// Content-Description
	headers.ContentDescription = getString("Content-Description")

	// Delivery-Status
	headers.DeliveryStatus = headerExists("Delivery-Status") ||
		headerExists("X-Failed-Recipients")

	// List-Unsubscribe
	headers.ListUnsubscribe = headerExists("List-Unsubscribe")

	// Precedence
	headers.Precedence = getString("Precedence")

	// Return-Path
	returnPath := getString("Return-Path")
	headers.ReturnPath = returnPath
	headers.ReturnPathExists = headerExists("Return-Path")

	// Auto-reply headers
	headers.XAutoreply = getString("X-Autoreply")
	headers.XAutoresponse = getString("X-Autoresponse")

	// X-Loop
	headers.XLoop = headerExists("X-Loop")

	// X-Failed-Recipients
	failedRecipientsStr := getString("X-Failed-Recipients")
	if failedRecipientsStr != "" {
		recipients := strings.Split(failedRecipientsStr, ",")
		for i, recipient := range recipients {
			recipients[i] = strings.TrimSpace(recipient)
		}
		headers.XFailedRecipients = recipients
	}

	// Reply-To
	headers.ReplyTo = getString("Reply-To")
	headers.ReplyToExists = headerExists("Reply-To")

	// Sender
	headers.Sender = getString("Sender")

	// Forwarded-For (could be in different formats)
	headers.ForwardedFor = getString("X-Forwarded-For")
	if headers.ForwardedFor == "" {
		headers.ForwardedFor = getString("Forwarded-For")
	}

	// Security headers
	headers.DKIM = getStringArray("DKIM-Signature")
	headers.SPF = getString("Received-SPF")
	headers.DMARC = getString("DMARC-Result")

	return headers, nil
}
