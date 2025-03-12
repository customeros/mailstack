package imap

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/opentracing/opentracing-go"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/tracing"
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
	INITIAL_SYNC_BATCH_SIZE = 50
	INITIAL_SYNC_MAX_TOTAL  = 50000
)

// SetEventHandler sets the event handler
func (s *IMAPService) SetEventHandler(handler func(context.Context, interfaces.MailEvent)) {
	s.eventHandler = handler
}

// Start initializes the service and connects to mailboxes
func (s *IMAPService) Start(ctx context.Context) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "IMAPService.Start")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	s.ctx, s.cancel = context.WithCancel(ctx)

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

	s.clientsMutex.Lock()
	defer s.clientsMutex.Unlock()

	// Check for duplicate
	if _, exists := s.configs[config.ID]; exists {
		err := fmt.Errorf("mailbox with ID %s already exists", config.ID)
		tracing.TraceErr(span, err)
		return err
	}

	// Store configuration
	s.configs[config.ID] = config

	// Update status
	s.updateStatus(config.ID, interfaces.MailboxStatus{
		Connected: false,
		Folders:   make(map[string]interfaces.FolderStats),
	})

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

	// Use simple reconnection logic
	backoff := time.Second
	maxBackoff := 2 * time.Minute
	attempts := 0

	for {
		// Create a span just for this connection attempt
		attemptSpan := opentracing.StartSpan("IMAPService.connectionAttempt")
		attemptSpan.SetTag("mailbox.id", mailboxID)
		attemptSpan.SetTag("attempt", attempts)

		attempts++
		log.Printf("[%s] Connection attempt #%d", mailboxID, attempts)

		// Check if we should stop
		select {
		case <-ctx.Done():
			attemptSpan.Finish() // Always finish spans
			log.Printf("[%s] Stopping mailbox monitoring due to context cancellation", mailboxID)
			return
		default:
			// Continue processing
		}

		// Use connection timeout
		connectCtx, connectCancel := context.WithTimeout(ctx, 1*time.Minute)

		// Connect to the mailbox
		client, err := s.connectSimple(connectCtx, config)
		connectCancel()

		if err != nil {
			log.Printf("[%s] Connection error: %v", mailboxID, err)
			s.updateStatusError(mailboxID, err)
			attemptSpan.LogKV("error", err.Error())
			attemptSpan.Finish() // Finish span here

			// Sleep with backoff before reconnecting
			select {
			case <-time.After(backoff):
				// Increase backoff for next attempt
				backoff = time.Duration(float64(backoff) * 1.5)
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				log.Printf("[%s] Will retry in %v", mailboxID, backoff)
			case <-ctx.Done():
				return
			}
			continue
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
		s.updateStatus(mailboxID, interfaces.MailboxStatus{
			Connected: true,
			Folders:   make(map[string]interfaces.FolderStats),
		})

		// Reset backoff on successful connection
		backoff = time.Second

		// Log the folders being processed
		attemptSpan.LogKV("folders", fmt.Sprintf("%v", config.SyncFolders))

		// Process each folder sequentially for easier debugging
		var folderError error

		for _, folder := range config.SyncFolders {
			folderName := string(folder)

			// Create a span just for this folder processing
			folderSpan := opentracing.StartSpan(
				"IMAPService.processFolder",
				opentracing.ChildOf(attemptSpan.Context()),
			)
			folderSpan.SetTag("mailbox.id", mailboxID)
			folderSpan.SetTag("folder.name", folderName)

			log.Printf("[%s] Processing folder: %s", mailboxID, folderName)

			// Use a timeout for folder processing
			folderCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			err := s.processFolder(folderCtx, client, mailboxID, folderName)
			cancel()

			if err != nil {
				log.Printf("[%s][%s] Error processing folder: %v", mailboxID, folderName, err)
				folderSpan.LogKV("error", err.Error())

				// Check if this is a connection error that should trigger reconnection
				if strings.Contains(err.Error(), "connection closed") ||
					strings.Contains(err.Error(), "i/o timeout") ||
					strings.Contains(err.Error(), "EOF") ||
					strings.Contains(err.Error(), "connection reset") {
					folderError = err
					log.Printf("[%s][%s] Connection error, will reconnect", mailboxID, folderName)
					folderSpan.Finish() // Finish span
					break               // Exit folder loop to trigger reconnection
				}

				folderSpan.Finish() // Finish span
				// Continue with next folder for other types of errors
			} else {
				folderSpan.LogKV("result", "success")
				folderSpan.Finish() // Finish span
			}
		}

		// Cleanup client if there was a connection error
		if folderError != nil {
			s.clientsMutex.Lock()
			delete(s.clients, mailboxID)
			s.clientsMutex.Unlock()

			s.updateStatus(mailboxID, interfaces.MailboxStatus{
				Connected: false,
				LastError: folderError.Error(),
				Folders:   make(map[string]interfaces.FolderStats),
			})

			// Use a shorter backoff for connection errors
			backoff = 5 * time.Second

			log.Printf("[%s] Will reconnect in %v after connection error", mailboxID, backoff)
			attemptSpan.LogKV("reconnect_reason", "connection_error")
			attemptSpan.Finish() // Finish span

			select {
			case <-time.After(backoff):
				// Continue with reconnection
			case <-ctx.Done():
				return
			}

			continue
		}

		// If we reach here, reconnect after a short delay
		log.Printf("[%s] Reconnecting after folder processing", mailboxID)
		attemptSpan.LogKV("reconnect_reason", "maintenance")
		attemptSpan.Finish() // Finish span

		select {
		case <-time.After(30 * time.Second):
			// Continue with reconnection
		case <-ctx.Done():
			return
		}
	}
}

// connectSimple establishes a connection to an IMAP server
func (s *IMAPService) connectSimple(ctx context.Context, config *models.Mailbox) (*client.Client, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "IMAPService.connectSimple")
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

// processFolder handles a single IMAP folder
func (s *IMAPService) processFolder(ctx context.Context, c *client.Client, mailboxID, folderName string) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "IMAPService.processFolder")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

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

	// Update folder stats
	s.updateFolderStats(ctx, mailboxID, folderName, mbox)

	// Get the last synchronized UID
	lastUID := s.getLastSyncedUID(ctx, mailboxID, folderName)

	if lastUID > 0 {
		// Sync new messages since last UID
		log.Printf("[%s][%s] Resuming sync from UID %d", mailboxID, folderName, lastUID)
		err = s.syncNewMessagesSince(ctx, c, mailboxID, folderName, lastUID)
		if err != nil {
			err = fmt.Errorf("error syncing new messages: %w", err)
			tracing.TraceErr(span, err)
			return err
		}
	} else {
		// Initial sync
		log.Printf("[%s][%s] Performing initial sync", mailboxID, folderName)
		err = s.performInitialSync(ctx, c, mailboxID, folderName)
		if err != nil {
			err = fmt.Errorf("error performing initial sync: %w", err)
			tracing.TraceErr(span, err)
			return err
		}
	}

	// Use simple polling instead of IDLE for easier debugging
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

			// Update folder stats
			s.updateFolderStats(ctx, mailboxID, folderName, mbox)

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

	// Update last synced UID if we found any messages
	if highestUID > 0 {
		s.saveLastSyncedUID(ctx, mailboxID, folderName, highestUID)
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
	if highestUID > 0 {
		s.saveLastSyncedUID(ctx, mailboxID, folderName, highestUID)
	}

	return nil
}

// performInitialSync does an initial sync of messages
func (s *IMAPService) performInitialSync(
	ctx context.Context,
	c *client.Client,
	mailboxID, folderName string,
) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "IMAPService.performInitialSync")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	// Search for unseen messages
	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{imap.SeenFlag}

	// Set timeout
	c.Timeout = 30 * time.Second
	uids, err := c.UidSearch(criteria)
	c.Timeout = 0

	if err != nil {
		err = fmt.Errorf("error searching for unseen messages: %w", err)
		tracing.TraceErr(span, err)
		return err
	}

	if len(uids) == 0 {
		log.Printf("[%s][%s] No unseen messages to sync", mailboxID, folderName)
		return nil
	}

	// Limit the number of messages
	maxToProcess := INITIAL_SYNC_MAX_TOTAL
	if len(uids) > maxToProcess {
		log.Printf("[%s][%s] Limiting initial sync to %d of %d messages",
			mailboxID, folderName, maxToProcess, len(uids))

		// Sort UIDs in descending order (newest first)
		sort.SliceStable(uids, func(i, j int) bool {
			return uids[i] > uids[j]
		})

		uids = uids[:maxToProcess]
	}

	log.Printf("[%s][%s] Starting initial sync of %d messages", mailboxID, folderName, len(uids))

	// Process in smaller batches
	batchSize := INITIAL_SYNC_BATCH_SIZE
	var highestUID uint32

	for i := 0; i < len(uids); i += batchSize {
		// Check for cancellation
		select {
		case <-ctx.Done():
			tracing.TraceErr(span, ctx.Err())
			return ctx.Err()
		default:
			// Continue processing
		}

		end := i + batchSize
		if end > len(uids) {
			end = len(uids)
		}

		batchUIDs := uids[i:end]
		log.Printf("[%s][%s] Processing batch %d-%d of %d",
			mailboxID, folderName, i+1, end, len(uids))

		// Create sequence set
		seqSet := new(imap.SeqSet)
		for _, uid := range batchUIDs {
			seqSet.AddNum(uid)
			if uint32(uid) > highestUID {
				highestUID = uint32(uid)
			}
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
		messageCount := 0

		for msg := range messages {
			messageCount++

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
			log.Printf("[%s][%s] Error processing batch: %v", mailboxID, folderName, err)
			// Continue with next batch instead of failing completely
		}

		log.Printf("[%s][%s] Processed %d messages in batch", mailboxID, folderName, messageCount)

		// Add a small delay between batches
		time.Sleep(100 * time.Millisecond)
	}

	// Save the highest UID
	if highestUID > 0 {
		s.saveLastSyncedUID(ctx, mailboxID, folderName, highestUID)
		log.Printf("[%s][%s] Updated last synced UID to %d", mailboxID, folderName, highestUID)
	}

	log.Printf("[%s][%s] Completed initial sync", mailboxID, folderName)
	return nil
}

// getLastSyncedUID gets the last synced UID for a folder
func (s *IMAPService) getLastSyncedUID(ctx context.Context, mailboxID, folderName string) uint32 {
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	state, err := s.repositories.MailboxSyncRepository.GetSyncState(timeoutCtx, mailboxID, folderName)
	if err != nil {
		log.Printf("[%s][%s] Error getting sync state: %v", mailboxID, folderName, err)
		return 0
	}

	if state == nil {
		return 0
	}

	return state.LastUID
}

// saveLastSyncedUID saves the last synced UID for a folder
func (s *IMAPService) saveLastSyncedUID(ctx context.Context, mailboxID, folderName string, uid uint32) {
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	state := &models.MailboxSyncState{
		MailboxID:  mailboxID,
		FolderName: folderName,
		LastUID:    uid,
		LastSync:   time.Now(),
	}

	err := s.repositories.MailboxSyncRepository.SaveSyncState(timeoutCtx, state)
	if err != nil {
		log.Printf("[%s][%s] Error saving sync state: %v", mailboxID, folderName, err)
	} else {
		log.Printf("[%s][%s] Updated last synced UID to %d", mailboxID, folderName, uid)
	}
}

// updateStatus updates the status of a mailbox
func (s *IMAPService) updateStatus(mailboxID string, status interfaces.MailboxStatus) {
	s.statusMutex.Lock()
	defer s.statusMutex.Unlock()
	s.statuses[mailboxID] = status
}

// updateStatusError updates the error status of a mailbox
func (s *IMAPService) updateStatusError(mailboxID string, err error) {
	s.statusMutex.Lock()
	defer s.statusMutex.Unlock()

	status, exists := s.statuses[mailboxID]
	if !exists {
		status = interfaces.MailboxStatus{
			Folders: make(map[string]interfaces.FolderStats),
		}
	}

	status.Connected = false
	status.LastError = err.Error()
	s.statuses[mailboxID] = status
}

// updateFolderStats updates the stats for a folder
func (s *IMAPService) updateFolderStats(ctx context.Context, mailboxID, folderName string, mbox *imap.MailboxStatus) {
	s.statusMutex.Lock()
	defer s.statusMutex.Unlock()

	status, exists := s.statuses[mailboxID]
	if !exists {
		status = interfaces.MailboxStatus{
			Connected: true,
			Folders:   make(map[string]interfaces.FolderStats),
		}
	}

	// Count unseen messages
	unseenCount := mbox.Unseen

	// Update folder stats
	status.Folders[folderName] = interfaces.FolderStats{
		Total:    mbox.Messages,
		Unseen:   unseenCount,
		LastSeen: s.getLastSyncedUID(ctx, mailboxID, folderName),
		LastSync: time.Now(),
	}

	s.statuses[mailboxID] = status
}
