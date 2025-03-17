package handlers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/customeros/mailsherpa/mailvalidate"
	go_imap "github.com/emersion/go-imap"
	"github.com/jhillyerd/enmime"
	"github.com/lib/pq"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"

	"github.com/customeros/mailstack/dto"
	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"
	"github.com/customeros/mailstack/services/events"
)

// IMAPHandler processes events from IMAP sources
type IMAPHandler struct {
	repositories       *repository.Repositories
	eventService       *events.EventsService
	emailFilterService interfaces.EmailFilterService
}

// NewIMAPHandler creates a new IMAP email handler
func NewIMAPHandler(
	repositories *repository.Repositories,
	eventService *events.EventsService,
	emailFilterService interfaces.EmailFilterService,
) *IMAPHandler {
	return &IMAPHandler{
		repositories:       repositories,
		eventService:       eventService,
		emailFilterService: emailFilterService,
	}
}

// Handle processes an IMAP email event
func (h *IMAPHandler) Handle(ctx context.Context, event interfaces.MailEvent) {
	span, ctx := tracing.StartTracerSpan(ctx, "IMAPHandler.Handle")
	defer span.Finish()
	tracing.LogObjectAsJson(span, "event", event)

	switch msg := event.Message.(type) {
	case *go_imap.Message:
		err := h.processIMAPMessage(ctx, event.MailboxID, event.Folder, msg)
		if err != nil {
			tracing.TraceErr(span, err)
			return
		}

	default:
		err := fmt.Errorf("  Unknown message type %T", msg)
		span.LogKV("messageId", event.MessageID)
		tracing.TraceErr(span, err)
		return
	}
}

// Main processing function
func (h *IMAPHandler) processIMAPMessage(ctx context.Context, mailboxID, folder string, msg *go_imap.Message) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "IMAPHandler.processIMAPMessage")
	defer span.Finish()

	email := models.Email{
		MailboxID:  mailboxID,
		Direction:  enum.EmailInbound,
		Status:     enum.EmailStatusReceived,
		Folder:     folder,
		ImapUID:    msg.Uid,
		ReceivedAt: utils.NowPtr(),
	}

	// Process envelope data
	h.processEnvelope(&email, msg.Envelope)

	// Process message content
	attachments := h.processMessageContent(&email, msg)

	err := h.emailFilterService.ScanEmail(ctx, &email)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	// attach message to thread
	err = h.attachMessageToThread(ctx, &email)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	// Save the email entity to the database
	_, err = h.repositories.EmailRepository.Create(ctx, &email)
	if err != nil {
		err = errors.Wrap(err, "Error saving email")
		return err
	}

	// Create attachment records if any
	if email.HasAttachment && len(attachments) > 0 {
		h.processAttachments(email.ID, attachments)
	}

	// Publish event for downstream processing
	if email.Classification == enum.EmailOK {
		return h.eventService.Publisher.PublishFanoutEvent(ctx, email.ID, enum.EMAIL, dto.EmailReceived{})
	}
	return nil
}

func (h *IMAPHandler) attachMessageToThread(ctx context.Context, email *models.Email) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "IMAPHandler.attachMessageToThread")
	defer span.Finish()

	// Step 1: Try to find existing thread by headers and references
	threadID, err := h.findExistingThread(ctx, email)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	// Step 2: Process based on whether we found a thread
	if threadID != "" {
		// Attach to existing thread
		email.ThreadID = threadID
		if err := h.updateThreadMetadata(ctx, email, threadID); err != nil {
			tracing.TraceErr(span, err)
			return err
		}
	} else {
		// Create new thread
		threadID, err = h.createNewThread(ctx, email)
		if err != nil {
			tracing.TraceErr(span, err)
			return err
		}
		email.ThreadID = threadID

		// Record missing parents if applicable
		if err := h.recordMissingParents(ctx, email); err != nil {
			tracing.TraceErr(span, err)
			return err
		}
	}

	return nil
}

// findExistingThread attempts to find an existing thread for the email
func (h *IMAPHandler) findExistingThread(ctx context.Context, email *models.Email) (string, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "IMAPHandler.findExistingThread")
	defer span.Finish()

	// Case 1: Check if this is a parent to a missing parent message
	if email.ReplyTo == "" && len(email.References) == 0 {
		orphan, err := h.repositories.OrphanEmailRepository.GetByMessageID(ctx, email.MessageID)
		if err != nil {
			tracing.TraceErr(span, err)
			return "", err
		}

		if orphan != nil && orphan.ThreadID != "" && orphan.MailboxID == email.MailboxID {
			// Clean up orphan records for this thread
			if err := h.repositories.OrphanEmailRepository.DeleteByThreadID(ctx, email.ThreadID); err != nil {
				tracing.TraceErr(span, err)
				return "", err
			}
			return orphan.ThreadID, nil
		}
		// No matching orphan found
		return "", nil
	}

	// Case 2: Check based on ReplyTo
	if email.ReplyTo != "" {
		threadID, err := h.findThreadByMessageID(ctx, email.ReplyTo)
		if err != nil {
			tracing.TraceErr(span, err)
			return "", err
		}
		if threadID != "" {
			return threadID, nil
		}
	}

	// Case 3: Check based on References
	if len(email.References) > 0 {
		for _, messageID := range email.References {
			threadID, err := h.findThreadByMessageID(ctx, messageID)
			if err != nil {
				tracing.TraceErr(span, err)
				return "", err
			}
			if threadID != "" {
				return threadID, nil
			}
		}
	}

	// Case 4: Try subject-based matching as a fallback
	normalizedSubject := utils.NormalizeEmailSubject(email.Subject)
	if normalizedSubject != "" {
		threadID, err := h.findThreadBySubjectAndParticipants(ctx, normalizedSubject, email.MailboxID, email.AllParticipants())
		if err != nil {
			tracing.TraceErr(span, err)
			// Just log this error and continue - subject matching is a best-effort fallback
			span.LogKV("warning", "subject-based thread matching failed", "error", err.Error())
		} else if threadID != "" {
			return threadID, nil
		}
	}

	// No existing thread found
	return "", nil
}

// findThreadByMessageID finds a thread containing a specific message ID
func (h *IMAPHandler) findThreadByMessageID(ctx context.Context, messageID string) (string, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "IMAPHandler.findThreadByMessageID")
	defer span.Finish()
	span.SetTag("message_id", messageID)

	message, err := h.repositories.EmailRepository.GetByMessageID(ctx, messageID)
	if err != nil {
		tracing.TraceErr(span, err)
		return "", err
	}
	if message == nil {
		return "", nil
	}
	return message.ThreadID, nil
}

// findThreadBySubjectAndParticipants finds a thread by normalized subject and participants
func (h *IMAPHandler) findThreadBySubjectAndParticipants(ctx context.Context, subject string, mailboxID string, participants []string) (string, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "IMAPHandler.findThreadBySubjectAndParticipants")
	defer span.Finish()
	span.SetTag("subject", subject)
	span.SetTag("mailbox_id", mailboxID)

	// Skip empty subjects
	if subject == "" {
		return "", nil
	}
	subject = utils.NormalizeSubject(subject)

	// Get threads matching the subject and mailbox
	threads, err := h.repositories.EmailThreadRepository.FindBySubjectAndMailbox(ctx, subject, mailboxID)
	if err != nil {
		tracing.TraceErr(span, err)
		return "", err
	}

	if len(threads) == 0 {
		return "", nil
	}

	// If only one thread matches, return it
	if len(threads) == 1 {
		return threads[0].ID, nil
	}

	// If multiple threads match, find the one with most participant overlap
	bestMatchThreadID := ""
	highestOverlap := 0

	for _, thread := range threads {
		// Calculate the number of participants that overlap
		overlap := 0
		for _, emailParticipant := range participants {
			if utils.IsStringInSlice(emailParticipant, thread.Participants) {
				overlap++
			}
		}

		// If this thread has more overlap than the previous best match, use it
		if overlap > highestOverlap {
			highestOverlap = overlap
			bestMatchThreadID = thread.ID
		}
	}

	// Only return a match if we have at least one participant overlap
	if highestOverlap > 0 {
		return bestMatchThreadID, nil
	}

	// No good match found
	return "", nil
}

// updateThreadMetadata updates thread metadata with data from the new email
func (h *IMAPHandler) updateThreadMetadata(ctx context.Context, email *models.Email, threadID string) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "IMAPHandler.updateThreadMetadata")
	defer span.Finish()

	// Get current thread
	threadRecord, err := h.repositories.EmailThreadRepository.GetByID(ctx, threadID)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}
	if threadRecord == nil {
		err = errors.New("thread record is unexpectedly nil")
		tracing.TraceErr(span, err)
		return err
	}

	// Update message count
	threadRecord.MessageCount++

	// Update attachments flag
	if email.HasAttachment {
		threadRecord.HasAttachments = true
	}

	// Update timestamps, safely handling nil cases
	if email.SentAt != nil {
		// Update first message time if this message is earlier
		if threadRecord.FirstMessageAt == nil || email.SentAt.Before(*threadRecord.FirstMessageAt) {
			threadRecord.FirstMessageAt = email.SentAt
		}

		// Update last message time if this message is later
		if threadRecord.LastMessageAt == nil || email.SentAt.After(*threadRecord.LastMessageAt) {
			threadRecord.LastMessageAt = email.SentAt
			threadRecord.LastMessageID = email.MessageID
		}
	}

	// Update participants
	newParticipants := email.AllParticipants()
	for _, participant := range newParticipants {
		if !utils.IsStringInSlice(participant, threadRecord.Participants) {
			threadRecord.Participants = append(threadRecord.Participants, participant)
		}
	}

	// Save thread updates
	return h.repositories.EmailThreadRepository.Update(ctx, threadRecord)
}

// createNewThread creates a new thread for the email
func (h *IMAPHandler) createNewThread(ctx context.Context, email *models.Email) (string, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "IMAPHandler.createNewThread")
	defer span.Finish()

	threadID, err := h.repositories.EmailThreadRepository.Create(ctx, &models.EmailThread{
		MailboxID:      email.MailboxID,
		Subject:        utils.NormalizeSubject(email.Subject),
		Participants:   email.AllParticipants(),
		MessageCount:   1,
		LastMessageID:  email.MessageID,
		HasAttachments: email.HasAttachment,
		FirstMessageAt: email.ReceivedAt,
		LastMessageAt:  email.ReceivedAt,
	})
	if err != nil {
		tracing.TraceErr(span, err)
		return "", err
	}

	return threadID, nil
}

// recordMissingParents records referenced messages that are missing
func (h *IMAPHandler) recordMissingParents(ctx context.Context, email *models.Email) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "IMAPHandler.recordMissingParents")
	defer span.Finish()

	// Record ReplyTo as missing parent if it exists
	if email.ReplyTo != "" {
		if _, err := h.repositories.OrphanEmailRepository.Create(ctx, &models.OrphanEmail{
			MessageID:    email.ReplyTo,
			ReferencedBy: email.MessageID,
			ThreadID:     email.ThreadID,
			MailboxID:    email.MailboxID,
		}); err != nil {
			tracing.TraceErr(span, err)
			return err
		}
	}

	// Record References as missing parents
	for _, messageID := range email.References {
		if _, err := h.repositories.OrphanEmailRepository.Create(ctx, &models.OrphanEmail{
			MessageID:    messageID,
			ReferencedBy: email.MessageID,
			ThreadID:     email.ThreadID,
			MailboxID:    email.MailboxID,
		}); err != nil {
			tracing.TraceErr(span, err)
			return err
		}
	}

	return nil
}

// Process envelope data - separate function for better readability
func (h *IMAPHandler) processEnvelope(email *models.Email, envelope *go_imap.Envelope) {
	if envelope == nil {
		return
	}

	// Basic envelope data
	if !envelope.Date.IsZero() {
		sentTime := envelope.Date
		email.SentAt = &sentTime
	}

	email.Subject = envelope.Subject
	email.CleanSubject = utils.NormalizeSubject(envelope.Subject)
	email.InReplyTo = envelope.InReplyTo
	email.MessageID = strings.Trim(envelope.MessageId, "<>")

	h.processInReplyTo(email, envelope)

	// Sender information
	if len(envelope.From) > 0 {
		sender := envelope.From[0]
		email.FromName = sender.PersonalName
		syntaxValidation := mailvalidate.ValidateEmailSyntax(sender.Address())
		if syntaxValidation.IsValid {
			email.FromAddress = syntaxValidation.CleanEmail
			email.FromDomain = syntaxValidation.Domain
			email.FromUser = syntaxValidation.User
		}
	}

	// Recipients
	email.ToAddresses = h.convertAddressesToStringArray(envelope.To)
	email.CcAddresses = h.convertAddressesToStringArray(envelope.Cc)
	email.BccAddresses = h.convertAddressesToStringArray(envelope.Bcc)

	// Store raw envelope data for reference
	envelopeMap := make(map[string]interface{})
	envelopeMap["date"] = envelope.Date
	envelopeMap["subject"] = envelope.Subject
	envelopeMap["message_id"] = envelope.MessageId
	envelopeMap["in_reply_to"] = envelope.InReplyTo
	envelopeMap["from"] = h.addressesToMap(envelope.From)
	envelopeMap["to"] = h.addressesToMap(envelope.To)
	envelopeMap["cc"] = h.addressesToMap(envelope.Cc)
	envelopeMap["bcc"] = h.addressesToMap(envelope.Bcc)
	email.Envelope = models.JSONMap(envelopeMap)
}

func (h *IMAPHandler) processInReplyTo(email *models.Email, envelope *go_imap.Envelope) {
	var allReferences []string

	// Process In-Reply-To (can contain multiple IDs space-separated)
	if envelope.InReplyTo != "" {
		inReplyToRefs := strings.Split(envelope.InReplyTo, " ")
		for _, ref := range inReplyToRefs {
			// Clean angle brackets
			ref = strings.Trim(ref, "<>")
			if ref != "" && !utils.IsStringInSlice(ref, allReferences) {
				allReferences = append(allReferences, ref)
			}
		}

		// Set the cleaned In-Reply-To (using first reference if multiple exist)
		if len(allReferences) > 0 {
			email.InReplyTo = allReferences[0] // Store without <>
		}
	}

	email.References = allReferences
}

func (h *IMAPHandler) processReferences(email *models.Email, headers map[string]interface{}) {
	var allReferences []string

	// Get references from headers
	referencesRaw, ok := headers["References"]
	if ok {
		var refsString string

		// Handle different possible types from the raw headers
		switch refs := referencesRaw.(type) {
		case []string:
			if len(refs) > 0 {
				refsString = refs[0]
			}
		case string:
			refsString = refs
		}

		if refsString != "" {
			// References can be space or newline separated
			refsString = strings.ReplaceAll(refsString, "\r\n", " ")
			refsString = strings.ReplaceAll(refsString, "\n", " ")

			// Split by space
			referencesList := strings.Split(refsString, " ")
			for _, ref := range referencesList {
				// Clean angle brackets
				ref = strings.Trim(ref, "<>")
				if ref != "" && !utils.IsStringInSlice(ref, allReferences) {
					allReferences = append(allReferences, ref)
				}
			}
		}
	}

	// Also add any existing references from email
	if email.References != nil {
		for _, reference := range email.References {
			if reference != "" && !utils.IsStringInSlice(reference, allReferences) {
				allReferences = append(allReferences, reference)
			}
		}
	}

	// Update email references
	email.References = pq.StringArray(allReferences)
}

// Process message content
func (h *IMAPHandler) processMessageContent(email *models.Email, msg *go_imap.Message) []map[string]interface{} {
	// Get the full message content
	fullMessageData := h.extractFullMessage(msg)

	if len(fullMessageData) > 0 {
		// Parse with enmime for better email parsing
		return h.parseWithEnmime(email, fullMessageData)
	} else {
		// Fallback to manual extraction
		return h.extractContentManually(email, msg)
	}
}

// Extract full message data
func (h *IMAPHandler) extractFullMessage(msg *go_imap.Message) []byte {
	var fullMessageBuffer bytes.Buffer

	for section, literal := range msg.Body {
		if section.Peek {
			continue // Skip PEEK sections to avoid duplicates
		}

		// Check if this is the full message section
		if len(section.Path) == 0 && section.Specifier == go_imap.EntireSpecifier {
			data, err := io.ReadAll(literal)
			if err == nil {
				fullMessageBuffer.Write(data)
				break
			}
		}
	}

	return fullMessageBuffer.Bytes()
}

// Parse message using enmime
func (h *IMAPHandler) parseWithEnmime(email *models.Email, messageData []byte) []map[string]interface{} {
	emailParser, err := enmime.ReadEnvelope(bytes.NewReader(messageData))
	if err != nil {
		log.Printf("Error parsing email with enmime: %v", err)
		return nil
	}

	// Extract headers
	headers := make(map[string]interface{})
	for _, key := range emailParser.GetHeaderKeys() {
		values := emailParser.GetHeaderValues(key)
		if len(values) > 0 {
			headers[key] = values
		}
	}

	h.processReferences(email, headers)

	email.RawHeaders = models.JSONMap(headers)

	// Extract body content
	email.BodyText = emailParser.Text
	email.BodyHTML = emailParser.HTML

	// Create body structure from enmime data
	bodyStructure := h.createBodyStructureFromEnmime(emailParser)
	email.BodyStructure = models.JSONMap(bodyStructure)

	// Process attachments
	attachments := make([]map[string]interface{}, 0)

	// Regular attachments
	for _, attachment := range emailParser.Attachments {
		attachmentInfo := map[string]interface{}{
			"filename":     attachment.FileName,
			"content_type": attachment.ContentType,
			"disposition":  attachment.Disposition,
			"size":         len(attachment.Content),
			"content":      attachment.Content, // Include the actual content
		}
		attachments = append(attachments, attachmentInfo)
	}

	// Inline attachments
	for _, inlineAttachment := range emailParser.Inlines {
		attachmentInfo := map[string]interface{}{
			"filename":     inlineAttachment.FileName,
			"content_type": inlineAttachment.ContentType,
			"disposition":  "inline",
			"content_id":   inlineAttachment.ContentID,
			"size":         len(inlineAttachment.Content),
			"content":      inlineAttachment.Content,
		}
		attachments = append(attachments, attachmentInfo)
	}

	if len(attachments) > 0 {
		email.HasAttachment = true
	}
	return attachments
}

// Helper function to create body structure from enmime data
func (h *IMAPHandler) createBodyStructureFromEnmime(emailParser *enmime.Envelope) map[string]interface{} {
	bodyStructure := make(map[string]interface{})

	// Add basic information
	bodyStructure["has_text"] = emailParser.Text != ""
	bodyStructure["has_html"] = emailParser.HTML != ""
	bodyStructure["has_attachments"] = len(emailParser.Attachments) > 0 || len(emailParser.Inlines) > 0

	// Add content types
	contentType := emailParser.GetHeader("Content-Type")
	if contentType != "" {
		bodyStructure["content_type"] = contentType
	}

	// Create parts array
	parts := []map[string]interface{}{}

	// Text part
	if emailParser.Text != "" {
		textPart := map[string]interface{}{
			"type":     "text/plain",
			"charset":  "UTF-8", // Default, could be extracted from Content-Type if available
			"encoding": emailParser.GetHeader("Content-Transfer-Encoding"),
			"size":     len(emailParser.Text),
		}
		parts = append(parts, textPart)
	}

	// HTML part
	if emailParser.HTML != "" {
		htmlPart := map[string]interface{}{
			"type":     "text/html",
			"charset":  "UTF-8", // Default, could be extracted from Content-Type if available
			"encoding": emailParser.GetHeader("Content-Transfer-Encoding"),
			"size":     len(emailParser.HTML),
		}
		parts = append(parts, htmlPart)
	}

	// Attachment parts
	for _, attachment := range emailParser.Attachments {
		attachmentPart := map[string]interface{}{
			"type":        attachment.ContentType,
			"filename":    attachment.FileName,
			"disposition": "attachment",
			"size":        len(attachment.Content),
		}
		parts = append(parts, attachmentPart)
	}

	// Inline parts
	for _, inline := range emailParser.Inlines {
		inlinePart := map[string]interface{}{
			"type":        inline.ContentType,
			"filename":    inline.FileName,
			"disposition": "inline",
			"content_id":  inline.ContentID,
			"size":        len(inline.Content),
		}
		parts = append(parts, inlinePart)
	}

	bodyStructure["parts"] = parts

	// Add metadata about MIME structure
	bodyStructure["is_multipart"] = len(parts) > 1

	return bodyStructure
}

// Manual content extraction as fallback
func (h *IMAPHandler) extractContentManually(email *models.Email, msg *go_imap.Message) []map[string]interface{} {
	// Store body structure if available
	var attachments []map[string]interface{}
	if msg.BodyStructure != nil {
		bodyStructure := h.parseBodyStructure(msg.BodyStructure)
		email.BodyStructure = models.JSONMap(bodyStructure)

		// Check for attachments in body structure
		attachments = h.extractAttachmentsFromStructure(msg.BodyStructure)
		if len(attachments) > 0 {
			email.HasAttachment = true
		}
	}

	// Try to extract content from individual parts
	for section, literal := range msg.Body {
		sectionKey := fmt.Sprintf("%v", section)

		data, err := io.ReadAll(literal)
		if err != nil {
			continue
		}

		// Extract text and HTML content
		if strings.Contains(strings.ToLower(sectionKey), "text/plain") {
			email.BodyText = string(data)
		} else if strings.Contains(strings.ToLower(sectionKey), "text/html") {
			email.BodyHTML = string(data)
		}
	}
	return attachments
}

// Process attachments - create attachment records
func (h *IMAPHandler) processAttachments(emailID string, attachmentsData []map[string]interface{}) {
	for _, attachmentData := range attachmentsData {
		// Create attachment entity
		attachment := models.EmailAttachment{
			EmailID:     emailID,
			Filename:    attachmentData["filename"].(string),
			ContentType: attachmentData["content_type"].(string),
			Size:        attachmentData["size"].(int),
			IsInline:    attachmentData["disposition"] == "inline",
		}

		// Set ContentID for inline attachments
		if attachment.IsInline && attachmentData["content_id"] != nil {
			attachment.ContentID = attachmentData["content_id"].(string)
		}

		// Save attachment metadata
		if err := h.repositories.EmailAttachmentRepository.Create(context.Background(), &attachment); err != nil {
			log.Printf("Error saving attachment: %v", err)
			continue
		}

		// Upload attachment content to storage
		if content, ok := attachmentData["content"].([]byte); ok && len(content) > 0 {
			err := h.repositories.EmailAttachmentRepository.Store(
				context.Background(),
				&attachment,
				content,
			)
			if err != nil {
				log.Printf("Error storing attachment content: %v", err)
			}
		}
	}
}

// Helper function to convert addresses to string array
func (h *IMAPHandler) convertAddressesToStringArray(addresses []*go_imap.Address) pq.StringArray {
	if len(addresses) == 0 {
		return pq.StringArray{}
	}

	result := make([]string, 0, len(addresses))
	for _, addr := range addresses {
		if addr.MailboxName != "" && addr.HostName != "" {
			emailAddr := addr.Address()
			validation := mailvalidate.ValidateEmailSyntax(emailAddr)
			if validation.IsValid {
				result = append(result, validation.CleanEmail)
			}
		}
	}

	return pq.StringArray(result)
}

// Helper to convert addresses to map for JSON storage
func (h *IMAPHandler) addressesToMap(addresses []*go_imap.Address) []map[string]string {
	result := make([]map[string]string, 0, len(addresses))

	for _, addr := range addresses {
		addrMap := map[string]string{
			"name":    addr.PersonalName,
			"address": addr.Address(),
		}
		result = append(result, addrMap)
	}

	return result
}

// Parse body structure recursively
func (h *IMAPHandler) parseBodyStructure(bs *go_imap.BodyStructure) map[string]interface{} {
	if bs == nil {
		return nil
	}

	result := make(map[string]interface{})

	result["mime_type"] = bs.MIMEType
	result["mime_subtype"] = bs.MIMESubType
	result["parameters"] = bs.Params
	result["id"] = bs.Id
	result["description"] = bs.Description
	result["encoding"] = bs.Encoding
	result["size"] = bs.Size
	result["lines"] = bs.Lines

	if bs.Disposition != "" {
		result["disposition"] = bs.Disposition
		result["disposition_params"] = bs.DispositionParams
	}

	if bs.Language != nil {
		result["language"] = bs.Language
	}

	if bs.Location != nil {
		result["location"] = bs.Location
	}

	if bs.MD5 != "" {
		result["md5"] = bs.MD5
	}

	if len(bs.Parts) > 0 {
		parts := make([]map[string]interface{}, 0, len(bs.Parts))
		for _, part := range bs.Parts {
			parts = append(parts, h.parseBodyStructure(part))
		}
		result["parts"] = parts
	}

	return result
}

// Extract attachments from body structure
func (h *IMAPHandler) extractAttachmentsFromStructure(bs *go_imap.BodyStructure) []map[string]interface{} {
	attachments := []map[string]interface{}{}

	// Check if this part is an attachment
	if bs.Disposition == "attachment" || bs.Disposition == "inline" {
		attachment := make(map[string]interface{})

		// Get filename
		filename := ""
		if bs.DispositionParams != nil {
			if fname, ok := bs.DispositionParams["filename"]; ok {
				filename = fname
			}
		}

		if filename == "" && bs.Params != nil {
			if name, ok := bs.Params["name"]; ok {
				filename = name
			}
		}

		if filename == "" {
			filename = fmt.Sprintf("attachment.%s", strings.ToLower(bs.MIMESubType))
		}

		attachment["filename"] = filename
		attachment["content_type"] = fmt.Sprintf("%s/%s", bs.MIMEType, bs.MIMESubType)
		attachment["size"] = bs.Size
		attachment["disposition"] = bs.Disposition

		attachments = append(attachments, attachment)
	}

	// Recursively check all parts
	if len(bs.Parts) > 0 {
		for _, part := range bs.Parts {
			partAttachments := h.extractAttachmentsFromStructure(part)
			attachments = append(attachments, partAttachments...)
		}
	}

	return attachments
}
