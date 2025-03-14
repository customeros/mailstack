package imap

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/opentracing/opentracing-go"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"
)

// performInitialSync orchestrates the initial sync of messages with resumption support
func (s *IMAPService) performInitialSync(
	ctx context.Context,
	c *client.Client,
	mailboxID, folderName string,
) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "IMAPService.performInitialSync")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	// Get all UIDs that need to be synced
	syncState, uidsToProcess, err := s.getUIDsToSync(ctx, c, mailboxID, folderName)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}
	if syncState == nil {
		err := errors.New("no sync state")
		tracing.TraceErr(span, err)
		return err
	}

	if len(uidsToProcess) == 0 {
		log.Printf("[%s][%s] No messages to sync", mailboxID, folderName)
		return nil
	}

	totalMessagesToProcess := len(uidsToProcess)
	log.Printf("[%s][%s] Starting initial sync of %d messages", mailboxID, folderName, totalMessagesToProcess)

	// Process in batches
	return s.processBatches(ctx, c, *syncState, uidsToProcess, totalMessagesToProcess)
}

// getUIDsToSync returns a slice of UIDs that need to be synced
func (s *IMAPService) getUIDsToSync(ctx context.Context, c *client.Client, mailboxID, folderName string,
) (*models.MailboxSyncState, []uint32, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "IMAPService.getUIDsToSync")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	// Get the last synced UID, if any (from previous incomplete sync)
	syncState, err := s.repositories.MailboxSyncRepository.GetSyncState(ctx, mailboxID, folderName)
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, nil, err
	}

	// Search for all messages in the folder
	criteria := imap.NewSearchCriteria()
	// Leave criteria empty to get all messages

	// Set timeout
	c.Timeout = 30 * time.Second
	allUIDs, err := c.UidSearch(criteria)
	c.Timeout = 0

	if err != nil {
		err = fmt.Errorf("error searching for messages: %w", err)
		return nil, nil, err
	}

	if len(allUIDs) == 0 {
		return nil, nil, nil
	}

	// Sort UIDs in ascending order (oldest first)
	sort.SliceStable(allUIDs, func(i, j int) bool {
		return allUIDs[i] < allUIDs[j]
	})

	// Check if we're resuming an incomplete sync
	var uidsToProcess []uint32
	if syncState.LastUID > 0 {
		log.Printf("[%s][%s] Resuming initial sync after UID %d", mailboxID, folderName, syncState.LastUID)
		// Filter UIDs to only include those greater than lastUID
		for _, uid := range allUIDs {
			if uid > syncState.LastUID {
				uidsToProcess = append(uidsToProcess, uid)
			}
		}
	} else {
		uidsToProcess = allUIDs
	}

	// Limit the number of messages if needed
	maxToProcess := INITIAL_SYNC_MAX_TOTAL
	if len(uidsToProcess) > maxToProcess {
		log.Printf("[%s][%s] Limiting initial sync to %d of %d messages",
			mailboxID, folderName, maxToProcess, len(uidsToProcess))
		uidsToProcess = uidsToProcess[:maxToProcess]
	}

	return syncState, uidsToProcess, nil
}

// processBatches processes UIDs in batches with state persistence
func (s *IMAPService) processBatches(
	ctx context.Context,
	c *client.Client,
	syncState models.MailboxSyncState,
	uidsToProcess []uint32,
	totalMessagesToProcess int,
) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "IMAPService.processBatches")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	batchSize := INITIAL_SYNC_BATCH_SIZE
	processedCount := 0

	for i := 0; i < len(uidsToProcess); i += batchSize {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		end := i + batchSize
		if end > len(uidsToProcess) {
			end = len(uidsToProcess)
		}

		batchUIDs := uidsToProcess[i:end]
		batchHighestUID := batchUIDs[len(batchUIDs)-1] // Last UID in batch is highest

		log.Printf("[%s][%s] Processing batch %d-%d of %d (UIDs %d-%d)",
			syncState.MailboxID, syncState.FolderName, processedCount+1, processedCount+len(batchUIDs), totalMessagesToProcess,
			batchUIDs[0], batchHighestUID)

		batchMessageCount, err := s.processSingleBatch(ctx, c, syncState.MailboxID, syncState.FolderName, batchUIDs)
		if err != nil {
			return err
		}

		// Update processed count
		processedCount += batchMessageCount

		log.Printf("[%s][%s] Successfully processed %d messages in batch (%d/%d total)",
			syncState.MailboxID, syncState.FolderName, batchMessageCount, processedCount, totalMessagesToProcess)

		// Save the highest UID after each batch to allow resumption
		syncState.LastUID = batchHighestUID
		syncState.LastSync = utils.Now()

		err = s.repositories.MailboxSyncRepository.SaveSyncState(ctx, &syncState)
		if err != nil {
			tracing.TraceErr(span, err)
			// Continue despite error saving state
		} else {
			log.Printf("[%s][%s] Saved batch progress (UID %d, %d/%d messages)",
				syncState.MailboxID, syncState.FolderName, batchHighestUID, processedCount, totalMessagesToProcess)
		}

		// Add a small delay between batches
		select {
		case <-time.After(1 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	log.Printf("[%s][%s] Completed initial sync of %d messages",
		syncState.MailboxID, syncState.FolderName, processedCount)
	return nil
}

// processSingleBatch processes a single batch of UIDs and returns the number of messages processed
func (s *IMAPService) processSingleBatch(
	ctx context.Context,
	c *client.Client,
	mailboxID, folderName string,
	batchUIDs []uint32,
) (int, error) {
	// Create sequence set
	seqSet := new(imap.SeqSet)
	for _, uid := range batchUIDs {
		seqSet.AddNum(uid)
	}

	// Fetch items
	items := []imap.FetchItem{
		imap.FetchEnvelope,
		imap.FetchFlags,
		imap.FetchBodyStructure,
		"BODY.PEEK[]",
		imap.FetchUid,
	}

	messages := make(chan *imap.Message, len(batchUIDs))
	done := make(chan error, 1)
	var wg sync.WaitGroup

	// Create error channel with buffer for all possible errors in this batch
	eventErrors := make(chan error, len(batchUIDs))

	// Set timeout for IMAP operation
	c.Timeout = 60 * time.Second

	// Start fetch
	go func() {
		done <- c.UidFetch(seqSet, items, messages)
	}()

	messageCount := s.processMessages(ctx, c, mailboxID, folderName, messages, &wg, eventErrors)

	// Reset IMAP timeout
	c.Timeout = 0

	// Check for IMAP fetch errors
	if err := <-done; err != nil {
		// Close error channel before returning
		close(eventErrors)
		log.Printf("[%s][%s] Error processing batch: %v", mailboxID, folderName, err)
		return 0, fmt.Errorf("IMAP fetch error: %w", err)
	}

	// Wait for completion and collect any errors
	err := s.waitForCompletion(ctx, mailboxID, folderName, &wg, eventErrors)
	if err != nil {
		return 0, err
	}

	return messageCount, nil
}

// processMessages processes messages from the channel and returns the count
func (s *IMAPService) processMessages(
	ctx context.Context,
	c *client.Client,
	mailboxID, folderName string,
	messages <-chan *imap.Message,
	wg *sync.WaitGroup,
	eventErrors chan<- error,
) int {
	// Create a semaphore to limit concurrent goroutines
	sem := make(chan struct{}, INITIAL_SYNC_BATCH_SIZE/2)
	messageCount := 0

	for msg := range messages {
		messageCount++

		select {
		case <-ctx.Done():
			return messageCount
		default:
		}

		wg.Add(1)

		// Process message in goroutine but track completion
		go func(msg *imap.Message) {
			defer wg.Done()

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			if s.eventHandler != nil {
				// Create context with timeout for event handler
				eventCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				defer cancel()

				// Handle the message
				func() {
					defer func() {
						if r := recover(); r != nil {
							select {
							case eventErrors <- fmt.Errorf("panic in event handler: %v", r):
							default:
								log.Printf("[%s][%s] Failed to send error: %v", mailboxID, folderName, r)
							}
						}
					}()

					s.eventHandler(eventCtx, interfaces.MailEvent{
						Source:    "imap",
						MailboxID: mailboxID,
						Folder:    folderName,
						MessageID: msg.SeqNum,
						EventType: "new",
						Message:   msg,
					})
				}()
			}
		}(msg)
	}

	return messageCount
}

// waitForCompletion waits for all goroutines to complete and collects errors
func (s *IMAPService) waitForCompletion(
	ctx context.Context,
	mailboxID, folderName string,
	wg *sync.WaitGroup,
	eventErrors chan error,
) error {
	// Wait for all event handlers to complete with timeout
	complete := make(chan struct{})
	go func() {
		wg.Wait()
		close(complete)
	}()

	// Collect any errors that occurred during processing
	var processingErrors []error

	// Start collecting errors
	collectDone := make(chan struct{})
	go func() {
		defer close(collectDone)
		for {
			select {
			case err, ok := <-eventErrors:
				if !ok {
					return // Channel closed
				}
				processingErrors = append(processingErrors, err)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Wait for completion or timeout
	var result error
	select {
	case <-complete:
		// All handlers completed
		close(eventErrors)

		// Wait for error collection to complete with timeout
		select {
		case <-collectDone:
			// Error collection complete
		case <-time.After(1 * time.Second):
			log.Printf("[%s][%s] Timeout waiting for error collection", mailboxID, folderName)
		}

		if len(processingErrors) > 0 {
			// Log all errors but return the first one
			for _, err := range processingErrors {
				log.Printf("[%s][%s] Batch processing error: %v", mailboxID, folderName, err)
			}
			result = fmt.Errorf("batch processing error: %w", processingErrors[0])
		}

	case <-time.After(2 * time.Minute):
		close(eventErrors)
		result = fmt.Errorf("timeout waiting for batch processing")

	case <-ctx.Done():
		close(eventErrors)
		result = ctx.Err()
	}

	return result
}
