package email_thread

import (
	"context"

	"github.com/pkg/errors"

	"github.com/customeros/mailstack/dto"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/telemetry"
	"github.com/customeros/mailstack/internal/utils"
)

func (s *EmailThreadingService) attachToThread(ctx context.Context, request dto.AttachToThreadRequest) dto.AttachToThreadResponse {
	spans, ctx := telemetry.StartServiceSpan(ctx, "EmailThreadingService.attachToThread")
	defer spans.Finish()
	spans.LogObjectAsJson("request", request)

	resp := dto.AttachToThreadResponse{
		EmailID:   request.EmailID,
		MessageID: request.MessageID,
	}

	// Step 1: Try to find existing thread by headers and references
	threadID, err := s.findExistingThread(ctx, request)
	if err != nil {
		spans.TraceError(err)
		resp.ErrorMessage = err.Error()
		return resp
	}

	if threadID != "" {
		resp.ThreadID = threadID
		return resp
	}

	// Step 2: Create new thread if none exists
	threadID, err = s.createNewThread(ctx, request)
	if err != nil {
		spans.TraceError(err)
		resp.ErrorMessage = err.Error()
		return resp
	}

	// Set thread ID on email
	resp.ThreadID = threadID

	// Record missing parents if applicable
	err = s.recordMissingParents(ctx, request, threadID)
	if err != nil {
		spans.TraceError(err)
		resp.ErrorMessage = err.Error()
		return resp
	}

	// Update thread metadata
	err = s.updateThreadMetadata(ctx, request, threadID)
	if err != nil {
		spans.TraceError(err)
		resp.ErrorMessage = err.Error()
		return resp
	}

	return resp
}

// findExistingThread attempts to find an existing thread for the email
func (s *EmailThreadingService) findExistingThread(ctx context.Context, request dto.AttachToThreadRequest) (string, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailProcessor.findExistingThread")
	defer spans.Finish()

	// Case 1: Check if this is a parent to a missing parent message
	threadID, err := s.checkForOrphanedParentMessage(ctx, request)
	if err != nil {
		return "", err
	}
	if threadID != "" {
		return threadID, nil
	}

	// Case 2: Check based on ReplyTo
	if request.ReplyTo != "" {
		threadID, err := s.findThreadByMessageID(ctx, request.ReplyTo)
		if err != nil {
			spans.TraceError(err)
			return "", err
		}

		if threadID != "" {
			return threadID, nil
		}
	}

	// Case 3: Check based on References
	for _, messageID := range request.References {
		threadID, err := s.findThreadByMessageID(ctx, messageID)
		if err != nil {
			spans.TraceError(err)
			return "", err
		}

		if threadID != "" {
			return threadID, nil
		}
	}

	// Case 4: Try subject-based matching as a fallback
	threadID, _ = s.findThreadBySubjectMatch(ctx, request)
	return threadID, nil
}

// checkForOrphanedParentMessage attempts to find a thread where this email is the parent of orphaned messages
func (s *EmailThreadingService) checkForOrphanedParentMessage(ctx context.Context, request dto.AttachToThreadRequest) (string, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "EmailThreadingService.checkForOrphanedParentMessage")
	defer spans.Finish()

	// Skip if this email is a reply or has references
	if request.ReplyTo != "" || len(request.References) > 0 {
		return "", nil
	}

	orphan, err := s.repositories.OrphanEmailRepository.GetByMessageID(ctx, request.MessageID)
	if err != nil {
		spans.TraceError(err)
		return "", err
	}

	// Return early if no matching orphan found
	if orphan == nil || orphan.ThreadID == "" || orphan.MailboxID != request.MailboxID {
		return "", nil
	}

	// Clean up orphan records for this thread
	err = s.repositories.OrphanEmailRepository.DeleteByThreadID(ctx, orphan.ThreadID)
	if err != nil {
		spans.TraceError(err)
		return "", err
	}

	return orphan.ThreadID, nil
}

// findThreadByMessageID finds a thread containing a specific message ID
func (s *EmailThreadingService) findThreadByMessageID(ctx context.Context, messageID string) (string, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "EmailThreadingService.findThreadByMessageID")
	defer spans.Finish()
	spans.LogKV("message_id", messageID)

	message, err := s.repositories.EmailRepository.GetByMessageID(ctx, messageID)
	if err != nil {
		spans.TraceError(err)
		return "", err
	}
	if message == nil {
		return "", nil
	}
	return message.ThreadID, nil
}

// findThreadBySubjectMatch attempts to find an existing thread by subject and participants
func (s *EmailThreadingService) findThreadBySubjectMatch(ctx context.Context, request dto.AttachToThreadRequest) (string, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "EmailThreadingService.findThreadBySubjectMatch")
	defer spans.Finish()

	normalizedSubject := utils.NormalizeSubject(request.Subject)
	if normalizedSubject == "" {
		return "", nil
	}

	threadID, err := s.findThreadBySubjectAndParticipants(ctx, normalizedSubject, request.MailboxID, request.AllParticipants)
	if err != nil {
		spans.TraceError(err)
		// Just log this error and continue - subject matching is a best-effort fallback
		spans.LogKV("warning", "subject-based thread matching failed")
		return "", nil
	}

	return threadID, nil
}

// findThreadBySubjectAndParticipants finds a thread by normalized subject and participants
func (s *EmailThreadingService) findThreadBySubjectAndParticipants(ctx context.Context, subject string, mailboxID string, participants []string) (string, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "EmailThreadingService.findThreadBySubjectAndParticipants")
	defer spans.Finish()
	spans.LogKV("subject", subject, "mailbox_id", mailboxID)

	// Skip empty subjects
	if subject == "" {
		return "", nil
	}

	// Get threads matching the subject and mailbox
	threads, err := s.repositories.EmailThreadRepository.FindBySubjectAndMailbox(ctx, subject, mailboxID)
	if err != nil {
		spans.TraceError(err)
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
func (s *EmailThreadingService) updateThreadMetadata(ctx context.Context, request dto.AttachToThreadRequest, threadID string) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "EmailThreadingService.updateThreadMetadata")
	defer spans.Finish()

	// Get current thread
	threadRecord, err := s.repositories.EmailThreadRepository.GetByID(ctx, threadID)
	if err != nil {
		spans.TraceError(err)
		return err
	}
	if threadRecord == nil {
		err = errors.New("thread record is unexpectedly nil")
		spans.TraceError(err)
		return err
	}

	// Update timestamps, safely handling nil cases
	if request.EmailSentAt != nil {
		// Update first message time if this message is earlier
		if threadRecord.FirstMessageAt == nil || request.EmailSentAt.Before(*threadRecord.FirstMessageAt) {
			threadRecord.FirstMessageAt = request.EmailSentAt
		}

		// Update last message time if this message is later
		if threadRecord.LastMessageAt == nil || request.EmailSentAt.After(*threadRecord.LastMessageAt) {
			threadRecord.LastMessageAt = request.EmailSentAt
			threadRecord.LastMessageID = request.MessageID
		}
	}

	// Update participants
	newParticipants := request.AllParticipants
	for _, participant := range newParticipants {
		if !utils.IsStringInSlice(participant, threadRecord.Participants) {
			threadRecord.Participants = append(threadRecord.Participants, participant)
		}
	}

	// Save thread updates
	return s.repositories.EmailThreadRepository.Update(ctx, threadRecord)
}

// createNewThread creates a new thread for the email
func (s *EmailThreadingService) createNewThread(ctx context.Context, request dto.AttachToThreadRequest) (string, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "EmailThreadingService.createNewThread")
	defer spans.Finish()

	threadID, err := s.repositories.EmailThreadRepository.Create(ctx, &models.EmailThread{
		MailboxID:      request.MailboxID,
		Subject:        utils.NormalizeSubject(request.Subject),
		Participants:   request.AllParticipants,
		LastMessageID:  request.MessageID,
		FirstMessageAt: request.EmailSentAt,
		LastMessageAt:  request.EmailSentAt,
	})
	if err != nil {
		spans.TraceError(err)
		return "", err
	}

	return threadID, nil
}

// recordMissingParents records referenced messages that are missing
func (s *EmailThreadingService) recordMissingParents(ctx context.Context, request dto.AttachToThreadRequest, threadID string) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "EmailThreadingService.recordMissingParents")
	defer spans.Finish()

	// Record ReplyTo as missing parent if it exists
	if request.ReplyTo != "" {
		if _, err := s.repositories.OrphanEmailRepository.Create(ctx, &models.OrphanEmail{
			MessageID:    request.ReplyTo,
			ReferencedBy: request.MessageID,
			ThreadID:     threadID,
			MailboxID:    request.MailboxID,
		}); err != nil {
			spans.TraceError(err)
			return err
		}
	}

	// Record References as missing parents
	for _, messageID := range request.References {
		if _, err := s.repositories.OrphanEmailRepository.Create(ctx, &models.OrphanEmail{
			MessageID:    messageID,
			ReferencedBy: request.MessageID,
			ThreadID:     threadID,
			MailboxID:    request.MailboxID,
		}); err != nil {
			spans.TraceError(err)
			return err
		}
	}

	return nil
}
