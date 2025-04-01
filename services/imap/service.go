package imap

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/pkg/errors"

	"github.com/customeros/mailstack/dto"
	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
	nats_internal "github.com/customeros/mailstack/internal/nats"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/telemetry"
	"github.com/customeros/mailstack/internal/utils"
	"github.com/customeros/mailstack/services/events"
)

type IMAPService struct {
	natsConn       *nats_internal.NATSConnections
	repositories   *repository.Repositories
	clients        map[string]*client.Client
	mailboxConfigs map[string]*models.Mailbox
	clientsMutex   sync.RWMutex
	wg             sync.WaitGroup
	ctx            context.Context
	cancel         context.CancelFunc
	statuses       map[string]interfaces.MailboxStatus
	statusMutex    sync.RWMutex
}

func NewIMAPService(nats *nats_internal.NATSConnections, repos *repository.Repositories) interfaces.IMAPService {
	return &IMAPService{
		natsConn:       nats,
		repositories:   repos,
		clients:        make(map[string]*client.Client),
		mailboxConfigs: make(map[string]*models.Mailbox),
		statuses:       make(map[string]interfaces.MailboxStatus),
	}
}

const (
	INITIAL_SYNC_BATCH_SIZE = 20
	INITIAL_SYNC_MAX_TOTAL  = 50000
)

// Start initializes the service and connects to mailboxes
func (s *IMAPService) Start(ctx context.Context) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "IMAPService.Start", telemetry.WithNewRoot())
	defer spans.Finish()

	s.ctx, s.cancel = context.WithCancel(ctx)
	spans.LogKV("mailbox_count", len(s.mailboxConfigs))

	// Start each mailbox sequentially for easier debugging
	for id, config := range s.mailboxConfigs {
		// Create a mailbox-specific context with tenant information
		// but don't create a span here since we're passing to a goroutine
		mailboxCtx := utils.SetTenantInContext(ctx, config.Tenant)

		log.Printf("Starting mailbox: %s (%s)", id, config.ImapUsername)
		go s.runSingleMailbox(mailboxCtx, id, config)
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
	spans, ctx := telemetry.StartServiceSpan(ctx, "IMAPService.AddMailbox")
	defer spans.Finish()

	if config == nil {
		err := errors.New("config is nil")
		spans.TraceError(err)
		return err
	}

	s.clientsMutex.Lock()
	defer s.clientsMutex.Unlock()

	// Check for duplicate
	if _, exists := s.mailboxConfigs[config.ID]; exists {
		err := fmt.Errorf("mailbox with ID %s already exists", config.ID)
		spans.TraceError(err)
		return err
	}

	if len(config.SyncFolders) == 0 {
		err := errors.New("sync folders is empty")
		spans.TraceError(err)
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
			spans.TraceError(err)
			return err
		}
	}

	// Store configuration
	s.mailboxConfigs[config.ID] = config

	// Start monitoring if service is running
	if s.ctx != nil {
		go s.runSingleMailbox(s.ctx, config.ID, config)
	}

	return nil
}

// RemoveMailbox removes a mailbox configuration
func (s *IMAPService) RemoveMailbox(ctx context.Context, mailboxID string) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "IMAPService.RemoveMailbox")
	defer spans.Finish()

	s.clientsMutex.Lock()
	defer s.clientsMutex.Unlock()

	// Disconnect if connected
	if client, exists := s.clients[mailboxID]; exists {
		client.Logout()
		delete(s.clients, mailboxID)
	}

	// Remove configuration
	delete(s.mailboxConfigs, mailboxID)
	err := s.repositories.MailboxSyncRepository.DeleteMailboxSyncStates(ctx, mailboxID)
	if err != nil {
		spans.TraceError(err)
		return err
	}

	// Remove status
	s.statusMutex.Lock()
	delete(s.statuses, mailboxID)
	s.statusMutex.Unlock()

	return nil
}

// getConnectedClient returns an established IMAP client for the given mailbox
// It will reuse an existing connection if available, or create a new one if needed
func (s *IMAPService) getConnectedClient(ctx context.Context, mailboxID string) (*client.Client, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "IMAPService.getConnectedClient")
	defer spans.Finish()
	spans.TagString("mailbox_id", mailboxID)

	// First check if we already have a connected client
	s.clientsMutex.RLock()
	existingClient, exists := s.clients[mailboxID]
	config, configExists := s.mailboxConfigs[mailboxID]
	s.clientsMutex.RUnlock()

	if !configExists {
		err := fmt.Errorf("no configuration found for mailbox %s", mailboxID)
		spans.TraceError(err)
		return nil, err
	}

	// If we have an existing client, check if it's still connected
	if exists {
		// Perform a simple NOOP operation to check connection health
		_, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		existingClient.Timeout = 10 * time.Second
		err := existingClient.Noop()
		existingClient.Timeout = 0

		if err == nil {
			// Connection is healthy, return it
			return existingClient, nil
		}

		// Connection is broken, log it
		log.Printf("[%s] Existing connection is broken, will establish a new one: %v", mailboxID, err)
		spans.LogKV("connection_status", "broken", "error", err)

		// Clean up the broken connection
		s.clientsMutex.Lock()
		delete(s.clients, mailboxID)
		s.clientsMutex.Unlock()
	}

	// We need to establish a new connection
	connectCtx, cancel := context.WithTimeout(ctx, 1*time.Minute)
	defer cancel()

	client, err := s.connectToIMAPServer(connectCtx, config)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	// Store the new client
	s.clientsMutex.Lock()
	s.clients[mailboxID] = client
	s.clientsMutex.Unlock()

	// Update connection status
	err = s.repositories.MailboxRepository.UpdateConnectionStatus(ctx, mailboxID, enum.ConnectionActive, "")
	if err != nil {
		// Log but continue since we have a working connection
		spans.TraceError(err)
		spans.LogKV("warning", "Failed to update connection status in repository")
	}

	return client, nil
}

// runSingleMailbox handles a single mailbox with reconnection
func (s *IMAPService) runSingleMailbox(ctx context.Context, mailboxID string, config *models.Mailbox) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "IMAPService.runSingleMailbox", telemetry.WithNewRoot())
	defer spans.Finish()
	spans.TagString("mailbox_id", mailboxID)
	spans.LogObjectAsJson("mailbox", config)

	// set tenant in context from mailbox if missing
	if utils.GetTenantFromContext(ctx) == "" {
		ctx = utils.SetTenantInContext(ctx, config.Tenant)
	}

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
	spans, ctx := telemetry.StartServiceSpan(ctx, "IMAPService.processSingleMailboxIteration", telemetry.WithNewRoot())
	defer spans.Finish()
	spans.TagString("mailbox_id", mailboxID)
	spans.LogKV("attempt", *attempts, "mailbox_username", config.ImapUsername)

	*attempts++
	log.Printf("[%s] Connection attempt #%d", mailboxID, *attempts)

	// Check if we should stop
	select {
	case <-ctx.Done():
		spans.TraceError(ctx.Err())
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
		spans.TraceError(err)
		err = s.repositories.MailboxRepository.UpdateConnectionStatus(ctx, mailboxID, enum.ConnectionNotActive, err.Error())
		if err != nil {
			spans.TraceError(err)
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
		spans.TraceError(err)
	}

	// Reset backoff on successful connection
	*backoff = time.Second

	// Log the folders being processed
	spans.LogKV("folders", fmt.Sprintf("%v", config.SyncFolders))

	// Process each folder sequentially
	_, connectivityError := s.syncFolders(ctx, client, mailboxID, config.SyncFolders)

	// Handle connectivity errors
	if connectivityError != nil {
		s.clientsMutex.Lock()
		delete(s.clients, mailboxID)
		s.clientsMutex.Unlock()

		err = s.repositories.MailboxRepository.UpdateConnectionStatus(ctx, mailboxID, enum.ConnectionNotActive, connectivityError.Error())
		if err != nil {
			spans.TraceError(err)
		}

		spans.TraceError(connectivityError)
		*backoff = 5 * time.Second
		return connectivityError
	}

	spans.LogKV("status", "cycle_complete")
	return nil
}

// connectToIMAPServer establishes a connection to an IMAP server
func (s *IMAPService) connectToIMAPServer(ctx context.Context, config *models.Mailbox) (*client.Client, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "IMAPService.connectToIMAPServer")
	defer spans.Finish()
	spans.TagString("mailbox_id", config.ID)

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
		spans.TraceError(err)
		return nil, err
	}

	// Check capabilities
	c.Timeout = 30 * time.Second
	caps, err := c.Capability()
	if err != nil {
		c.Logout()
		err := fmt.Errorf("capability error: %w", err)
		spans.TraceError(err)
		return nil, err
	}

	log.Printf("[%s] Server capabilities: %v", config.ID, caps)

	// Login
	err = c.Login(config.ImapUsername, config.ImapPassword)
	if err != nil {
		c.Logout()
		err := fmt.Errorf("login error: %w", err)
		spans.TraceError(err)
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
	spans, ctx := telemetry.StartServiceSpan(ctx, "IMAPService.processSingleFolder")
	defer spans.Finish()
	spans.TagString("folder", folder)

	log.Printf("[%s] Processing folder: %s", mailboxID, folder)

	// Use a timeout for folder processing
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel() // Ensure cancel is always called

	err := s.processFolder(ctx, client, mailboxID, folder)
	if err != nil {
		log.Printf("[%s][%s] Error processing folder: %v", mailboxID, folder, err)
		spans.TraceError(err)
		return err
	}

	spans.LogKV("result.status", "success")
	log.Printf("[%s][%s] Successfully processed folder", mailboxID, folder)
	return nil
}

// processFolder handles a single IMAP folder
func (s *IMAPService) processFolder(ctx context.Context, c *client.Client, mailboxID, folderName string) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "IMAPService.processFolder")
	defer spans.Finish()
	spans.TagString("mailbox_id", mailboxID)
	spans.TagString("folder", folderName)

	// Check for nil client
	if c == nil {
		err := fmt.Errorf("IMAP client is nil")
		spans.TraceError(err)
		return err
	}

	// Select the folder
	c.Timeout = 30 * time.Second
	mbox, err := c.Select(folderName, false)
	c.Timeout = 0
	if err != nil {
		err = fmt.Errorf("error selecting folder: %w", err)
		spans.TraceError(err)
		return err
	}

	log.Printf("[%s][%s] Selected folder - Messages: %d, Recent: %d, Unseen: %d",
		mailboxID, folderName, mbox.Messages, mbox.Recent, mbox.Unseen)

	// Get the last synchronized UID
	syncState, err := s.repositories.MailboxSyncRepository.GetSyncState(ctx, mailboxID, folderName)
	if err != nil {
		spans.TraceError(err)
		return err
	}

	if syncState == nil || syncState.LastUID == 0 {
		// Initial sync (no previous sync state or LastUID is 0)
		log.Printf("[%s][%s] Performing initial sync", mailboxID, folderName)
		err = s.performInitialSync(ctx, c, mailboxID, folderName)
		if err != nil {
			err = fmt.Errorf("error performing initial sync: %w", err)
			spans.TraceError(err)
			return err
		}
	} else {
		// We have a previous sync state, sync new messages
		log.Printf("[%s][%s] Resuming sync from UID %d", mailboxID, folderName, syncState.LastUID)
		err = s.syncNewMessagesSince(ctx, c, mailboxID, folderName, syncState.LastUID)
		if err != nil {
			err = fmt.Errorf("error syncing new messages: %w", err)
			spans.TraceError(err)
			return err
		}
	}

	// Use simple polling instead of IDLE for easier debugging
	log.Printf("[%s][%s] Starting polling after sync", mailboxID, folderName)
	return s.simplePolling(ctx, c, mailboxID, folderName)
}

// simplePolling periodically checks for new messages
func (s *IMAPService) simplePolling(ctx context.Context, c *client.Client, mailboxID, folderName string) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "IMAPService.simplePolling")
	defer spans.Finish()
	spans.TagString("mailbox_id", mailboxID)
	spans.TagString("folder", folderName)

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
					spans.TraceError(err)
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
					spans.TraceError(err)
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
						spans.TraceError(err)
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
	spans, ctx := telemetry.StartServiceSpan(ctx, "IMAPService.fetchNewMesssages")
	defer spans.Finish()
	spans.TagString("mailbox_id", mailboxID)
	spans.TagString("folder", folderName)

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
		event := dto.EmailReceivedIMAP{
			Source:      enum.EmailImportIMAP,
			MailboxID:   mailboxID,
			Folder:      folderName,
			ImapSeqNum:  msg.SeqNum,
			ImapUID:     msg.Uid,
			InitialSync: false,
		}
		err := s.publishNewEmailEvent(ctx, &event)
		if err != nil {
			spans.TraceError(err)
			return err
		}
	}

	// Reset timeout
	c.Timeout = 0

	// Check for fetch errors
	err := <-done
	if err != nil {
		err = fmt.Errorf("error fetching messages: %w", err)
		spans.TraceError(err)
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
		spans.TraceError(err)
		return err
	}

	return nil
}

func (s *IMAPService) publishNewEmailEvent(ctx context.Context, event *dto.EmailReceivedIMAP) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "IMAPService.publishNewEmailEvent")
	defer spans.Finish()
	// Convert to JSON
	data, err := json.Marshal(event)
	if err != nil {
		spans.TraceError(err)
		return fmt.Errorf("failed to marshal stored email: %w", err)
	}

	// Publish to the stored subject
	_, err = s.natsConn.JS.Publish(enum.EventEmailInboundReceivedIMAP.String(), data)
	if err != nil {
		spans.TraceError(err)
		return fmt.Errorf("failed to publish stored email: %w", err)
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
	spans, ctx := telemetry.StartServiceSpan(ctx, "IMAPService.syncNewMessagesSince")
	defer spans.Finish()
	spans.TagString("mailbox_id", mailboxID)
	spans.TagString("folder", folderName)

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
		spans.TraceError(err)
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
		event := dto.EmailReceivedIMAP{
			Source:      enum.EmailImportIMAP,
			MailboxID:   mailboxID,
			Folder:      folderName,
			ImapSeqNum:  msg.SeqNum,
			ImapUID:     msg.Uid,
			InitialSync: false,
		}
		err := s.publishNewEmailEvent(ctx, &event)
		if err != nil {
			spans.TraceError(err)
			return err
		}
	}

	// Reset timeout
	c.Timeout = 0

	// Check for fetch errors
	err = <-done
	if err != nil {
		err = fmt.Errorf("error fetching messages: %w", err)
		spans.TraceError(err)
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
		spans.TraceError(err)
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
