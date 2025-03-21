package email_processor

import (
	"context"

	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"

	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"
)

func (p *emailProcessor) attachEmailToThread(ctx context.Context, email *models.Email) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailProcessor.attachMessageToThread")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	// Step 1: Try to find existing thread by headers and references
	threadID, err := p.findExistingThread(ctx, email)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	// Step 2: Process based on thread existence
	if threadID == "" {
		// Create new thread if none exists
		threadID, err = p.createNewThread(ctx, email)
		if err != nil {
			tracing.TraceErr(span, err)
			return err
		}

		// Set thread ID on email
		email.ThreadID = threadID

		// Record missing parents if applicable
		err = p.recordMissingParents(ctx, email)
		if err != nil {
			tracing.TraceErr(span, err)
			return err
		}

		return nil
	}

	// Attach to existing thread
	email.ThreadID = threadID

	// Update thread metadata
	err = p.updateThreadMetadata(ctx, email, threadID)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	return nil
}

// findExistingThread attempts to find an existing thread for the email
func (p *emailProcessor) findExistingThread(ctx context.Context, email *models.Email) (string, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailProcessor.findExistingThread")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	// Case 1: Check if this is a parent to a missing parent message
	threadID, err := p.checkForOrphanedParentMessage(ctx, email)
	if err != nil {
		return "", err
	}
	if threadID != "" {
		return threadID, nil
	}

	// Case 2: Check based on ReplyTo
	if email.ReplyTo != "" {
		threadID, err := p.findThreadByMessageID(ctx, email.ReplyTo)
		if err != nil {
			tracing.TraceErr(span, err)
			return "", err
		}

		if threadID != "" {
			return threadID, nil
		}
	}

	// Case 3: Check based on References
	for _, messageID := range email.References {
		threadID, err := p.findThreadByMessageID(ctx, messageID)
		if err != nil {
			tracing.TraceErr(span, err)
			return "", err
		}

		if threadID != "" {
			return threadID, nil
		}
	}

	// Case 4: Try subject-based matching as a fallback
	threadID, _ = p.findThreadBySubjectMatch(ctx, email)
	return threadID, nil
}

// checkForOrphanedParentMessage attempts to find a thread where this email is the parent of orphaned messages
func (p *emailProcessor) checkForOrphanedParentMessage(ctx context.Context, email *models.Email) (string, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailProcessor.checkForOrphanedParentMessage")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	// Skip if this email is a reply or has references
	if email.ReplyTo != "" || len(email.References) > 0 {
		return "", nil
	}

	orphan, err := p.repositories.OrphanEmailRepository.GetByMessageID(ctx, email.MessageID)
	if err != nil {
		tracing.TraceErr(span, err)
		return "", err
	}

	// Return early if no matching orphan found
	if orphan == nil || orphan.ThreadID == "" || orphan.MailboxID != email.MailboxID {
		return "", nil
	}

	// Clean up orphan records for this thread
	err = p.repositories.OrphanEmailRepository.DeleteByThreadID(ctx, orphan.ThreadID)
	if err != nil {
		tracing.TraceErr(span, err)
		return "", err
	}

	return orphan.ThreadID, nil
}

// findThreadByMessageID finds a thread containing a specific message ID
func (p *emailProcessor) findThreadByMessageID(ctx context.Context, messageID string) (string, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailProcessor.findThreadByMessageID")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)
	span.SetTag("message_id", messageID)

	message, err := p.repositories.EmailRepository.GetByMessageID(ctx, messageID)
	if err != nil {
		tracing.TraceErr(span, err)
		return "", err
	}
	if message == nil {
		return "", nil
	}
	return message.ThreadID, nil
}

// findThreadBySubjectMatch attempts to find an existing thread by subject and participants
func (p *emailProcessor) findThreadBySubjectMatch(ctx context.Context, email *models.Email) (string, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailProcessor.findThreadBySubjectMatch")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	normalizedSubject := utils.NormalizeEmailSubject(email.Subject)
	if normalizedSubject == "" {
		return "", nil
	}

	threadID, err := p.findThreadBySubjectAndParticipants(ctx, normalizedSubject, email.MailboxID, email.AllParticipants())
	if err != nil {
		tracing.TraceErr(span, err)
		// Just log this error and continue - subject matching is a best-effort fallback
		span.LogKV("warning", "subject-based thread matching failed", "error", err.Error())
		return "", nil
	}

	return threadID, nil
}

// findThreadBySubjectAndParticipants finds a thread by normalized subject and participants
func (p *emailProcessor) findThreadBySubjectAndParticipants(ctx context.Context, subject string, mailboxID string, participants []string) (string, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailProcessor.findThreadBySubjectAndParticipants")
	defer span.Finish()
	span.SetTag("subject", subject)
	span.SetTag("mailbox_id", mailboxID)

	// Skip empty subjects
	if subject == "" {
		return "", nil
	}

	// Get threads matching the subject and mailbox
	threads, err := p.repositories.EmailThreadRepository.FindBySubjectAndMailbox(ctx, subject, mailboxID)
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
func (p *emailProcessor) updateThreadMetadata(ctx context.Context, email *models.Email, threadID string) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailProcesssor.updateThreadMetadata")
	defer span.Finish()

	// Get current thread
	threadRecord, err := p.repositories.EmailThreadRepository.GetByID(ctx, threadID)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}
	if threadRecord == nil {
		err = errors.New("thread record is unexpectedly nil")
		tracing.TraceErr(span, err)
		return err
	}

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
	return p.repositories.EmailThreadRepository.Update(ctx, threadRecord)
}

// createNewThread creates a new thread for the email
func (p *emailProcessor) createNewThread(ctx context.Context, email *models.Email) (string, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailProcesssor.createNewThread")
	defer span.Finish()

	threadID, err := p.repositories.EmailThreadRepository.Create(ctx, &models.EmailThread{
		MailboxID:      email.MailboxID,
		Subject:        utils.NormalizeSubject(email.Subject),
		Participants:   email.AllParticipants(),
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
func (p *emailProcessor) recordMissingParents(ctx context.Context, email *models.Email) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailProcesssor.recordMissingParents")
	defer span.Finish()

	// Record ReplyTo as missing parent if it exists
	if email.ReplyTo != "" {
		if _, err := p.repositories.OrphanEmailRepository.Create(ctx, &models.OrphanEmail{
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
		if _, err := p.repositories.OrphanEmailRepository.Create(ctx, &models.OrphanEmail{
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
