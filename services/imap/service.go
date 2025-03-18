package imap

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/opentracing/opentracing-go"
	tracingLog "github.com/opentracing/opentracing-go/log"
	"github.com/pkg/errors"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"
)

type IMAPService struct {
	repositories *repository.Repositories
	clients      map[string]*client.Client
	configs      map[string]*models.Mailbox
	eventHandler func(context.Context, interfaces.MailEvent)
	clientsMutex sync.RWMutex
	wg           sync.WaitGroup
	ctx          context.Context
	cancel       context.CancelFunc
	statuses     map[string]interfaces.MailboxStatus
	statusMutex  sync.RWMutex
}

func NewIMAPService(repos *repository.Repositories) interfaces.IMAPService {
	return &IMAPService{
		repositories: repos,
		clients:      make(map[string]*client.Client),
		configs:      make(map[string]*models.Mailbox),
		statuses:     make(map[string]interfaces.MailboxStatus),
	}
}

const (
	DEFAULT_IMAP_LOGOUT     = 25 // minutes
	DEFAULT_POLLING_PERIOD  = 20 // minutes
	INITIAL_SYNC_BATCH_SIZE = 20
	INITIAL_SYNC_MAX_TOTAL  = 50000
)

// SetEventHandler sets the event handler
func (s *IMAPService) SetEventHandler(handler func(context.Context, interfaces.MailEvent)) {
	s.eventHandler = handler
}

// Start initializes the service and connects to mailboxes
func (s *IMAPService) Start(ctx context.Context) error {
	span, ctx := tracing.StartTracerSpan(ctx, "IMAPService.Start")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	s.ctx, s.cancel = context.WithCancel(ctx)
	span.LogFields(tracingLog.Int("mailbox_count", len(s.configs)))

	// Start each mailbox sequentially for easier debugging
	for id, config := range s.configs {
		log.Printf("Starting mailbox: %s (%s)", id, config.ImapUsername)
		go s.runSingleMailbox(s.ctx, id, config)
	}

	return nil
}

// Stop gracefully shuts down the service
func (s *IMAPService) Stop() error {
	log.Println("Stopping IMAP service...")

	// Cancel main context to signal all operations to stop
	if s.cancel != nil {
		s.cancel()
	}

	// Wait for everything to finish with a timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("All IMAP operations completed gracefully")
	case <-time.After(10 * time.Second):
		log.Println("Timeout waiting for IMAP operations to complete")
	}

	// Disconnect all clients
	s.clientsMutex.Lock()
	for id, c := range s.clients {
		log.Printf("Disconnecting client: %s", id)
		// Set timeout for logout
		c.Timeout = 5 * time.Second
		_ = c.Logout() // Ignore errors during shutdown
		delete(s.clients, id)
	}
	s.clientsMutex.Unlock()

	log.Println("IMAP service stopped")
	return nil
}

// Status returns the current status of all mailboxes
func (s *IMAPService) Status() map[string]interfaces.MailboxStatus {
	s.statusMutex.RLock()
	defer s.statusMutex.RUnlock()

	// Return a copy to avoid race conditions
	result := make(map[string]interfaces.MailboxStatus)
	for id, status := range s.statuses {
		result[id] = status
	}

	return result
}

// AddMailbox adds a new mailbox configuration
func (s *IMAPService) AddMailbox(ctx context.Context, config *models.Mailbox) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "IMAPService.AddMailbox")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	if config == nil {
		err := errors.New("config is nil")
		tracing.TraceErr(span, err)
		return err
	}

	s.clientsMutex.Lock()
	defer s.clientsMutex.Unlock()

	// Check for duplicate
	if _, exists := s.configs[config.ID]; exists {
		err := fmt.Errorf("mailbox with ID %s already exists", config.ID)
		tracing.TraceErr(span, err)
		return err
	}

	if len(config.SyncFolders) == 0 {
		err := errors.New("sync folders is empty")
		tracing.TraceErr(span, err)
		return err
	}

	// Add initial entry into mailbox sync table
	for _, folder := range config.SyncFolders {
		err := s.repositories.MailboxSyncRepository.SaveSyncState(ctx, &models.MailboxSyncState{
			MailboxID:  config.ID,
			FolderName: folder,
			LastUID:    0,
		})
		if err != nil {
			tracing.TraceErr(span, err)
			return err
		}
	}

	// Store configuration
	s.configs[config.ID] = config

	// Start monitoring if service is running
	if s.ctx != nil {
		go s.runSingleMailbox(s.ctx, config.ID, config)
	}

	return nil
}

// RemoveMailbox removes a mailbox configuration
func (s *IMAPService) RemoveMailbox(ctx context.Context, mailboxID string) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "IMAPService.RemoveMailbox")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	s.clientsMutex.Lock()
	defer s.clientsMutex.Unlock()

	// Disconnect if connected
	if client, exists := s.clients[mailboxID]; exists {
		client.Logout()
		delete(s.clients, mailboxID)
	}

	// Remove configuration
	delete(s.configs, mailboxID)
	err := s.repositories.MailboxSyncRepository.DeleteMailboxSyncStates(ctx, mailboxID)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	// Remove status
	s.statusMutex.Lock()
	delete(s.statuses, mailboxID)
	s.statusMutex.Unlock()

	return nil
}

// runSingleMailbox handles a single mailbox with reconnection
func (s *IMAPService) runSingleMailbox(ctx context.Context, mailboxID string, config *models.Mailbox) {
	s.wg.Add(1)
	defer s.wg.Done()

	log.Printf("[%s] Starting mailbox monitoring with folders: %v", mailboxID, config.SyncFolders)

	backoff := time.Second
	maxBackoff := 2 * time.Minute
	attempts := 0

	for {
		if err := s.processSingleMailboxIteration(ctx, mailboxID, config, &attempts, &backoff, maxBackoff); err != nil {
			// If context is cancelled, we should exit
			if errors.Is(err, context.Canceled) {
				return
			}
			// Other errors are handled within processSingleMailboxIteration
			continue
		}

		// If we reach here, reconnect after a short delay
		select {
		case <-time.After(30 * time.Second):
			// Continue with reconnection
		case <-ctx.Done():
			return
		}
	}
}

// processSingleMailboxIteration handles a single iteration of mailbox processing
func (s *IMAPService) processSingleMailboxIteration(
	ctx context.Context,
	mailboxID string,
	config *models.Mailbox,
	attempts *int,
	backoff *time.Duration,
	maxBackoff time.Duration,
) error {
	// Create a new span for each iteration of the connection loop
	span, ctx := tracing.StartTracerSpan(ctx, "IMAPService.processSingleMailboxIteration")
	defer span.Finish()
	span.SetTag("mailbox.id", mailboxID)
	span.LogFields(tracingLog.Int("attempt", *attempts))
	span.LogFields(tracingLog.String("mailbox.username", config.ImapUsername))

	*attempts++
	log.Printf("[%s] Connection attempt #%d", mailboxID, *attempts)

	// Check if we should stop
	select {
	case <-ctx.Done():
		tracing.TraceErr(span, ctx.Err())
		log.Printf("[%s] Stopping mailbox monitoring due to context cancellation", mailboxID)
		return ctx.Err()
	default:
		// Continue processing
	}

	// Use connection timeout
	connectCtx, connectCancel := context.WithTimeout(ctx, 1*time.Minute)
	defer connectCancel()

	// Connect to the mailbox
	client, err := s.connectToIMAPServer(connectCtx, config)
	if err != nil {
		log.Printf("[%s] Connection error: %v", mailboxID, err)
		tracing.TraceErr(span, err)
		err = s.repositories.MailboxRepository.UpdateConnectionStatus(ctx, mailboxID, enum.ConnectionNotActive, err.Error())
		if err != nil {
			tracing.TraceErr(span, err)
		}

		// Sleep with backoff before reconnecting
		select {
		case <-time.After(*backoff):
			// Increase backoff for next attempt
			*backoff = time.Duration(float64(*backoff) * 1.5)
			if *backoff > maxBackoff {
				*backoff = maxBackoff
			}
			log.Printf("[%s] Will retry in %v", mailboxID, *backoff)
		case <-ctx.Done():
			return ctx.Err()
		}
		return err
	}

	// Store the client
	s.clientsMutex.Lock()
	// First check if there's an existing client to clean up
	if existingClient, exists := s.clients[mailboxID]; exists {
		existingClient.Timeout = 5 * time.Second
		go existingClient.Logout() // Ignore errors in a goroutine
	}
	s.clients[mailboxID] = client
	s.clientsMutex.Unlock()

	// Update status
	err = s.repositories.MailboxRepository.UpdateConnectionStatus(ctx, mailboxID, enum.ConnectionActive, "")
	if err != nil {
		tracing.TraceErr(span, err)
	}

	// Reset backoff on successful connection
	*backoff = time.Second

	// Log the folders being processed
	span.LogFields(tracingLog.String("folders", fmt.Sprintf("%v", config.SyncFolders)))

	// Process each folder sequentially
	_, connectivityError := s.syncFolders(ctx, client, config.ID, config.SyncFolders)

	// Handle connectivity errors
	if connectivityError != nil {
		s.clientsMutex.Lock()
		delete(s.clients, mailboxID)
		s.clientsMutex.Unlock()

		err = s.repositories.MailboxRepository.UpdateConnectionStatus(ctx, mailboxID, enum.ConnectionNotActive, connectivityError.Error())
		if err != nil {
			tracing.TraceErr(span, err)
		}

		tracing.TraceErr(span, connectivityError)
		*backoff = 5 * time.Second
		return connectivityError
	}

	span.LogFields(tracingLog.String("status", "cycle_complete"))
	return nil
}

// connectToIMAPServer establishes a connection to an IMAP server
func (s *IMAPService) connectToIMAPServer(ctx context.Context, config *models.Mailbox) (*client.Client, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "IMAPService.connectToIMAPServer")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	// Format server address
	serverAddr := fmt.Sprintf("%s:%d", config.ImapServer, config.ImapPort)

	// Set up connection with timeout
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	// Connect with or without TLS
	var c *client.Client
	var err error

	if config.ImapSecurity == enum.EmailSecurityTLS {
		tlsConfig := &tls.Config{
			ServerName: config.ImapServer,
		}
		c, err = client.DialWithDialerTLS(dialer, serverAddr, tlsConfig)
	} else {
		c, err = client.DialWithDialer(dialer, serverAddr)
	}

	if err != nil {
		err := fmt.Errorf("connection error: %w", err)
		tracing.TraceErr(span, err)
		return nil, err
	}

	// Check capabilities
	c.Timeout = 30 * time.Second
	caps, err := c.Capability()
	if err != nil {
		c.Logout()
		err := fmt.Errorf("capability error: %w", err)
		tracing.TraceErr(span, err)
		return nil, err
	}

	log.Printf("[%s] Server capabilities: %v", config.ID, caps)

	// Login
	err = c.Login(config.ImapUsername, config.ImapPassword)
	if err != nil {
		c.Logout()
		err := fmt.Errorf("login error: %w", err)
		tracing.TraceErr(span, err)
		return nil, err
	}

	// Reset timeout
	c.Timeout = 0

	log.Printf("[%s] Successfully connected to %s", config.ID, serverAddr)
	return c, nil
}

// syncFolders processes all folders and returns information about the sync process
func (s *IMAPService) syncFolders(
	ctx context.Context,
	client *client.Client,
	mailboxID string,
	folders []string,
) (processedFolders map[string]bool, connectivityError error) {
	processedFolders = make(map[string]bool)

	log.Printf("[%s] Starting sync for %d folders: %v", mailboxID, len(folders), folders)

	for _, folder := range folders {
		log.Printf("[%s] About to process folder: %s", mailboxID, folder)

		err := s.processSingleFolder(ctx, client, mailboxID, folder)
		if err != nil {
			if isConnectionError(err) {
				connectivityError = err
				log.Printf("[%s][%s] Connection error, will stop processing folders", mailboxID, folder)
				break
			}

			// Non-connectivity error, log and continue
			log.Printf("[%s][%s] Non-connectivity error, continuing with other folders", mailboxID, folder)
		}

		processedFolders[folder] = err == nil
	}

	return processedFolders, connectivityError
}

// processSingleFolder handles the logic for processing a single folder
func (s *IMAPService) processSingleFolder(
	ctx context.Context,
	client *client.Client,
	mailboxID string,
	folder string,
) error {
	folderSpan, folderCtx := opentracing.StartSpanFromContext(ctx, "IMAPService.processSingleFolder")
	defer folderSpan.Finish()
	folderSpan.LogFields(tracingLog.String("folder", folder))

	log.Printf("[%s] Processing folder: %s", mailboxID, folder)

	// Use a timeout for folder processing
	folderCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel() // Ensure cancel is always called

	err := s.processFolder(folderCtx, client, mailboxID, folder)
	if err != nil {
		log.Printf("[%s][%s] Error processing folder: %v", mailboxID, folder, err)
		tracing.TraceErr(folderSpan, err)
		return err
	}

	folderSpan.LogFields(tracingLog.String("result.status", "success"))
	log.Printf("[%s][%s] Successfully processed folder", mailboxID, folder)
	return nil
}

// processFolder handles a single IMAP folder
func (s *IMAPService) processFolder(ctx context.Context, c *client.Client, mailboxID, folderName string) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "IMAPService.processFolder")
	defer span.Finish()
	tracing.TagEntity(span, mailboxID)
	span.LogFields(tracingLog.String("folder", folderName))
	tracing.SetDefaultServiceSpanTags(ctx, span)

	// Check for nil client
	if c == nil {
		err := fmt.Errorf("IMAP client is nil")
		tracing.TraceErr(span, err)
		return err
	}

	// Select the folder
	c.Timeout = 30 * time.Second
	mbox, err := c.Select(folderName, false)
	c.Timeout = 0
	if err != nil {
		err = fmt.Errorf("error selecting folder: %w", err)
		tracing.TraceErr(span, err)
		return err
	}

	log.Printf("[%s][%s] Selected folder - Messages: %d, Recent: %d, Unseen: %d",
		mailboxID, folderName, mbox.Messages, mbox.Recent, mbox.Unseen)

	// Get the last synchronized UID
	syncState, err := s.repositories.MailboxSyncRepository.GetSyncState(ctx, mailboxID, folderName)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	if syncState == nil || syncState.LastUID == 0 {
		// Initial sync (no previous sync state or LastUID is 0)
		log.Printf("[%s][%s] Performing initial sync", mailboxID, folderName)
		err = s.performInitialSync(ctx, c, mailboxID, folderName)
		if err != nil {
			err = fmt.Errorf("error performing initial sync: %w", err)
			tracing.TraceErr(span, err)
			return err
		}
	} else {
		// We have a previous sync state, sync new messages
		log.Printf("[%s][%s] Resuming sync from UID %d", mailboxID, folderName, syncState.LastUID)
		err = s.syncNewMessagesSince(ctx, c, mailboxID, folderName, syncState.LastUID)
		if err != nil {
			err = fmt.Errorf("error syncing new messages: %w", err)
			tracing.TraceErr(span, err)
			return err
		}
	}

	// Use simple polling instead of IDLE for easier debugging
	log.Printf("[%s][%s] Starting polling after sync", mailboxID, folderName)
	return s.simplePolling(ctx, c, mailboxID, folderName)
}

// simplePolling periodically checks for new messages
func (s *IMAPService) simplePolling(ctx context.Context, c *client.Client, mailboxID, folderName string) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "IMAPService.simplePolling")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	log.Printf("[%s][%s] Starting simple polling", mailboxID, folderName)

	// Use a shorter polling interval to keep the connection alive
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	var lastCount uint32
	firstRun := true
	lastActivity := time.Now()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-ticker.C:
			// Check if connection has been idle for too long
			if time.Since(lastActivity) > 4*time.Minute {
				// Perform a NOOP to keep the connection alive
				_, noopCancel := context.WithTimeout(ctx, 10*time.Second)
				c.Timeout = 10 * time.Second

				log.Printf("[%s][%s] Connection idle for %v, performing NOOP",
					mailboxID, folderName, time.Since(lastActivity))

				err := c.Noop()
				c.Timeout = 0
				noopCancel()

				if err != nil {
					log.Printf("[%s][%s] NOOP failed, connection likely broken: %v",
						mailboxID, folderName, err)
					err = fmt.Errorf("connection health check failed: %w", err)
					tracing.TraceErr(span, err)
					return err
				}

				// NOOP succeeded, update activity time
				lastActivity = time.Now()
				continue
			}

			// Select the folder to get current status
			_, selectCancel := context.WithTimeout(ctx, 30*time.Second)
			c.Timeout = 30 * time.Second

			mbox, err := c.Select(folderName, false)
			c.Timeout = 0
			selectCancel()

			// Update activity timestamp on any successful operation
			lastActivity = time.Now()

			if err != nil {
				log.Printf("[%s][%s] Error selecting folder during poll: %v",
					mailboxID, folderName, err)

				// If we see a connection closed error, break out of polling loop
				if err.Error() == "imap: connection closed" ||
					strings.Contains(err.Error(), "i/o timeout") ||
					strings.Contains(err.Error(), "connection reset") {
					err = fmt.Errorf("connection lost: %w", err)
					tracing.TraceErr(span, err)
					return err
				}

				continue
			}

			// Check for new messages (skip first run to establish baseline)
			if !firstRun && mbox.Messages > lastCount {
				newCount := mbox.Messages - lastCount
				log.Printf("[%s][%s] Poll detected %d new message(s)",
					mailboxID, folderName, newCount)

				// Fetch new messages with timeout context
				fetchCtx, fetchCancel := context.WithTimeout(ctx, 2*time.Minute)

				err := s.fetchNewMessages(fetchCtx, c, mailboxID, folderName,
					lastCount+1, mbox.Messages)

				fetchCancel()

				if err != nil {
					log.Printf("[%s][%s] Error fetching new messages: %v",
						mailboxID, folderName, err)

					// Check if this is a connection error
					if strings.Contains(err.Error(), "connection closed") ||
						strings.Contains(err.Error(), "i/o timeout") ||
						strings.Contains(err.Error(), "connection reset") {
						err = fmt.Errorf("connection lost during fetch: %w", err)
						tracing.TraceErr(span, err)
						return err
					}
				}

				// Update activity timestamp
				lastActivity = time.Now()
			}

			lastCount = mbox.Messages
			firstRun = false
		}
	}
}

// fetchNewMessages fetches messages by sequence number
func (s *IMAPService) fetchNewMessages(
	ctx context.Context,
	c *client.Client,
	mailboxID, folderName string,
	from, to uint32,
) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "IMAPService.fetchNewMesssages")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	if from > to {
		return nil
	}

	log.Printf("[%s][%s] Fetching messages %d to %d", mailboxID, folderName, from, to)

	// Create sequence set
	seqSet := new(imap.SeqSet)
	seqSet.AddRange(from, to)

	// Fetch items
	items := []imap.FetchItem{
		imap.FetchEnvelope,
		imap.FetchFlags,
		imap.FetchBodyStructure,
		"BODY.PEEK[]",
		imap.FetchUid,
	}

	// Create message channel
	messages := make(chan *imap.Message, 10)
	done := make(chan error, 1)

	// Set timeout
	c.Timeout = 60 * time.Second

	// Start fetch
	go func() {
		done <- c.Fetch(seqSet, items, messages)
	}()

	// Process messages
	var highestUID uint32
	messageCount := 0

	for msg := range messages {
		messageCount++

		if msg.Uid > highestUID {
			highestUID = msg.Uid
		}

		// Process the message
		if s.eventHandler != nil {
			s.eventHandler(ctx, interfaces.MailEvent{
				Source:    "imap",
				MailboxID: mailboxID,
				Folder:    folderName,
				MessageID: msg.SeqNum,
				EventType: "new",
				Message:   msg,
			})
		}
	}

	// Reset timeout
	c.Timeout = 0

	// Check for fetch errors
	err := <-done
	if err != nil {
		err = fmt.Errorf("error fetching messages: %w", err)
		tracing.TraceErr(span, err)
		return err
	}

	log.Printf("[%s][%s] Processed %d messages", mailboxID, folderName, messageCount)

	// Update last synced UID
	if highestUID == 0 {
		return nil
	}

	err = s.repositories.MailboxSyncRepository.SaveSyncState(ctx, &models.MailboxSyncState{
		MailboxID:  mailboxID,
		FolderName: folderName,
		LastUID:    highestUID,
		LastSync:   utils.Now(),
	})
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	return nil
}

// syncNewMessagesSince syncs messages with UID greater than lastUID
func (s *IMAPService) syncNewMessagesSince(
	ctx context.Context,
	c *client.Client,
	mailboxID, folderName string,
	lastUID uint32,
) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "IMAPService.syncNewMessagesSince")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	// Create search criteria for UIDs greater than lastUID
	criteria := imap.NewSearchCriteria()
	uidRange := new(imap.SeqSet)
	uidRange.AddRange(lastUID+1, 0) // From lastUID+1 to infinity
	criteria.Uid = uidRange

	// Set timeout
	c.Timeout = 30 * time.Second
	uids, err := c.UidSearch(criteria)
	c.Timeout = 0

	if err != nil {
		err = fmt.Errorf("error searching for new messages: %w", err)
		tracing.TraceErr(span, err)
		return err
	}

	if len(uids) == 0 {
		log.Printf("[%s][%s] No new messages since UID %d", mailboxID, folderName, lastUID)
		return nil
	}

	log.Printf("[%s][%s] Found %d new messages since UID %d", mailboxID, folderName, len(uids), lastUID)

	// Create sequence set
	seqSet := new(imap.SeqSet)
	for _, uid := range uids {
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

	// Create message channel
	messages := make(chan *imap.Message, 10)
	done := make(chan error, 1)

	// Set timeout
	c.Timeout = 60 * time.Second

	// Start fetch
	go func() {
		done <- c.UidFetch(seqSet, items, messages)
	}()

	// Process messages
	var highestUID uint32
	messageCount := 0

	for msg := range messages {
		messageCount++

		if msg.Uid > highestUID {
			highestUID = msg.Uid
		}

		// Process the message
		if s.eventHandler != nil {
			s.eventHandler(ctx, interfaces.MailEvent{
				Source:    "imap",
				MailboxID: mailboxID,
				Folder:    folderName,
				MessageID: msg.SeqNum,
				EventType: "new",
				Message:   msg,
			})
		}
	}

	// Reset timeout
	c.Timeout = 0

	// Check for fetch errors
	err = <-done
	if err != nil {
		err = fmt.Errorf("error fetching messages: %w", err)
		tracing.TraceErr(span, err)
		return err
	}

	log.Printf("[%s][%s] Processed %d new messages", mailboxID, folderName, messageCount)

	// Update last synced UID
	if highestUID == 0 {
		return nil
	}

	err = s.repositories.MailboxSyncRepository.SaveSyncState(ctx, &models.MailboxSyncState{
		MailboxID:  mailboxID,
		FolderName: folderName,
		LastUID:    highestUID,
		LastSync:   utils.Now(),
	})
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	return nil
}

// isConnectionError checks if an error is related to connectivity
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}

	errorMsg := err.Error()
	return strings.Contains(errorMsg, "connection closed") ||
		strings.Contains(errorMsg, "i/o timeout") ||
		strings.Contains(errorMsg, "EOF") ||
		strings.Contains(errorMsg, "connection reset")
}
