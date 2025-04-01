package email_content

func (p *ImapProcessor) ProcessIMAPMessage(ctx context.Context, msg *imap.Message) (dto.EmailRecord, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "ImapProcessor.ProcessIMAPMessage")
	defer spans.Finish()

	email := p.EmailProcessor.NewInboundEmail()

	// Process envelope data
	processEnvelope(email, msg.Envelope)

	// Process message content
	headers, attachments := processMessageContent(email, msg)

	// Create attachment records if any
	if !email.HasAttachment || len(attachments) == 0 {
		return p.EmailProcessor.ProcessEmail(ctx, email, nil, nil)
	}

	attachmentRecords, files := p.processAttachments(attachments)

	return p.EmailProcessor.ProcessEmail(ctx, email, attachmentRecords, files)
}

func buildEmailEvent(ctx context.Context, email *dto.EmailRecord) *models.EmailEvent {
	return &models.EmailEvent{
		Timestamp:      utils.Now(),
		Tenant:         utils.GetTenantFromContext(ctx),
		EmailID:        email.ID,
		MailboxID:      email.MailboxID,
		MessageID:      email.MessageID,
		ThreadID:       email.ThreadID,
		FromAddress:    email.FromAddress,
		FromUser:       email.FromUser,
		FromDomain:     email.FromDomain,
		Recipients:     email.Recipients(),
		EmailKey:       email.EmailKey,
		Subject:        email.Subject,
		Direction:      string(email.Direction),
		Classification: string(email.Classification),
		SentAt:         email.SentAt,
		ReceivedAt:     email.ReceivedAt,
		ScheduledFor:   email.ScheduledFor,
	}
}

func processEnvelope(email *dto.EmailRecord, envelope *go_imap.Envelope) {
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

	processInReplyTo(email, envelope)

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
	email.ToAddresses = convertAddressesToStringArray(envelope.To)
	email.CcAddresses = convertAddressesToStringArray(envelope.Cc)
	email.BccAddresses = convertAddressesToStringArray(envelope.Bcc)

	// Store raw envelope data for reference
	envelopeMap := make(map[string]interface{})
	envelopeMap["date"] = envelope.Date
	envelopeMap["subject"] = envelope.Subject
	envelopeMap["message_id"] = envelope.MessageId
	envelopeMap["in_reply_to"] = envelope.InReplyTo
	envelopeMap["from"] = addressesToMap(envelope.From)
	envelopeMap["to"] = addressesToMap(envelope.To)
	envelopeMap["cc"] = addressesToMap(envelope.Cc)
	envelopeMap["bcc"] = addressesToMap(envelope.Bcc)
}

func processInReplyTo(email *dto.EmailRecord, envelope *go_imap.Envelope) {
	var allReferences []string

	// Process In-Reply-To (can contain multiple IDs space-separated)
	if envelope.InReplyTo == "" {
		return
	}

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

	email.References = allReferences
}

func convertAddressesToStringArray(addresses []*go_imap.Address) pq.StringArray {
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

func addressesToMap(addresses []*go_imap.Address) []map[string]string {
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

func parseBodyStructure(bs *go_imap.BodyStructure) map[string]interface{} {
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
			parts = append(parts, parseBodyStructure(part))
		}
		result["parts"] = parts
	}

	return result
}

func extractAttachmentsFromStructure(bs *go_imap.BodyStructure) []map[string]interface{} {
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
			partAttachments := extractAttachmentsFromStructure(part)
			attachments = append(attachments, partAttachments...)
		}
	}

	return attachments
}

func processMessageContent(email *dto.EmailRecord, msg *go_imap.Message) (headers map[string]interface{}, attachments []map[string]interface{}) {
	// Get the full message content
	fullMessageData := extractFullMessage(msg)

	if len(fullMessageData) > 0 {
		return parseWithEnmime(email, fullMessageData)
	}
	return nil, nil
}

// Extract full message data
func extractFullMessage(msg *go_imap.Message) []byte {
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
func parseWithEnmime(email *dto.EmailRecord, messageData []byte) (headers map[string]interface{}, attachments []map[string]interface{}) {
	emailParser, err := enmime.ReadEnvelope(bytes.NewReader(messageData))
	if err != nil {
		return nil, nil
	}

	// Extract headers
	headers = make(map[string]interface{})
	for _, key := range emailParser.GetHeaderKeys() {
		values := emailParser.GetHeaderValues(key)
		if len(values) > 0 {
			headers[key] = values
		}
	}

	processReferences(email, headers)

	// Extract body content
	email.BodyText = emailParser.Text
	email.BodyHTML = emailParser.HTML

	// Create body structure from enmime data
	createBodyStructureFromEnmime(emailParser)

	// Process attachments
	attachments = make([]map[string]interface{}, 0)

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
	return headers, attachments
}

// Helper function to create body structure from enmime data
func createBodyStructureFromEnmime(emailParser *enmime.Envelope) map[string]interface{} {
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

func processReferences(email *dto.EmailRecord, headers map[string]interface{}) {
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

func (p *ImapProcessor) processAttachments(attachmentsData []map[string]interface{}) ([]*models.EmailAttachment, []*interfaces.AttachmentFile) {
	var attachments []*models.EmailAttachment
	var files []*interfaces.AttachmentFile
	for _, attachmentData := range attachmentsData {
		attachment, attachmentFiles := p.processAttachment(attachmentData)
		attachments = append(attachments, attachment)
		for _, file := range attachmentFiles {
			files = append(files, file)
		}
	}
	return attachments, files
}

func (p *ImapProcessor) processAttachment(attachmentData map[string]interface{}) (*models.EmailAttachment, []*interfaces.AttachmentFile) {
	attachment := p.EmailProcessor.NewAttachment()
	attachment.Filename = attachmentData["filename"].(string)
	attachment.ContentType = attachmentData["content_type"].(string)
	attachment.Size = attachmentData["size"].(int)
	attachment.IsInline = attachmentData["disposition"] == "inline"

	// Set ContentID for inline attachments
	if attachment.IsInline && attachmentData["content_id"] != nil {
		attachment.ContentID = attachmentData["content_id"].(string)
	}

	// Process files
	var files []*interfaces.AttachmentFile
	content, ok := attachmentData["content"].([]byte)
	if ok && len(content) > 0 {
		files = append(files, p.EmailProcessor.NewAttachmentFile(attachment.ID, content))
	}

	return attachment, files
}

func (p *emailProcessor) getStructuredMessageBody(ctx context.Context, email *dto.EmailRecord) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailProcessor.getStructuredMessageBody")
	defer spans.Finish()
	spans.LogKV("email_id", email.ID)

	structuredData, err := p.aiService.GetStructuredEmailBody(ctx, dto.StructuredEmailRequest{
		FromName:         email.FromName,
		FromEmailAddress: email.FromAddress,
		ToEmailAddress:   email.ToAddresses[0],
		EmailBodyText:    email.BodyText,
		EmailBodyHTML:    email.BodyHTML,
	})
	if err != nil {
		spans.TraceError(err)
		return nil
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
		spans.TraceError(err)
		return err
	}

	return nil
}
