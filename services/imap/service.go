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
	"github.com/nats-io/nats.go"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
	nats_internal "github.com/customeros/mailstack/internal/nats"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/telemetry"
	"github.com/customeros/mailstack/internal/utils"
	"github.com/customeros/mailstack/proto/pb"
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
	googleService  interfaces.GoogleService
	podID          string
}

func NewIMAPService(nats *nats_internal.NATSConnections, repos *repository.Repositories, googleSvc interfaces.GoogleService) interfaces.IMAPService {
	return &IMAPService{
		natsConn:       nats,
		repositories:   repos,
		clients:        make(map[string]*client.Client),
		mailboxConfigs: make(map[string]*models.Mailbox),
		statuses:       make(map[string]interfaces.MailboxStatus),
		googleService:  googleSvc,
	}
}

const (
	SYNC_EMAILS_BATCH_SIZE_PER_ITERATION = 20
	SYNC_EMAILS_ITERATIONS_PER_ACQUIRE   = 500
)

// Start initializes the service and connects to mailboxes
func (s *IMAPService) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)

	// Generate unique pod ID
	s.podID = utils.GenerateNanoIDWithPrefix("pod", 16)
	log.Printf("Starting IMAP service with pod ID: %s", s.podID)

	// Start the mailbox processing loop
	go func() {
		for {
			// Look for mailboxes to process
			mailboxes, err := s.repositories.MailboxRepository.GetMailboxesForSync(ctx, 2*time.Minute)
			if err != nil {
				log.Printf("Error finding available mailboxes: %v", err)
				continue
			}

			for _, mailbox := range mailboxes {
				if !s.AcceptMailboxForSync(ctx, mailbox) {
					continue
				}

				// Try to acquire lock
				acquired, err := s.repositories.MailboxRepository.AcquireMailboxLock(ctx, mailbox.ID, s.podID, 5*time.Minute)
				if err != nil {
					log.Printf("Error acquiring lock for mailbox %s: %v", mailbox.ID, err)
					continue
				}

				if acquired {
					s.mailboxConfigs[mailbox.ID] = mailbox

					// Create mailbox-specific context with tenant information
					mailboxCtx := utils.SetTenantInContext(s.ctx, mailbox.Tenant)
					mailboxCtx = utils.SetUserIdInContext(mailboxCtx, mailbox.UserID)

					log.Printf("Starting mailbox: %s (%s)", mailbox.ID, mailbox.ImapUsername)
					go s.runSingleMailbox(mailboxCtx, mailbox.ID, mailbox)
					time.Sleep(50 * time.Millisecond)
				}
			}
			select {
			case <-time.After(30 * time.Second):
			case <-ctx.Done():
				return
			}
		}
	}()

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

	utils.SetTenantInContext(ctx, config.Tenant)
	utils.SetUserIdInContext(ctx, config.UserID)

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

// releaseMailbox releases the lock for a mailbox
func (s *IMAPService) releaseMailbox(ctx context.Context, mailboxID string) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "IMAPService.releaseMailbox")
	defer spans.Finish()
	spans.TagEntity(mailboxID)

	s.clientsMutex.Lock()
	defer s.clientsMutex.Unlock()

	err := s.repositories.MailboxRepository.ReleaseMailboxLock(ctx, mailboxID, s.podID)
	if err != nil {
		spans.TraceError(err)
		log.Printf("[%s] Failed to release lock: %v", mailboxID, err)
		spans.TraceError(err)
	} else {
		log.Printf("[%s] Successfully released mailbox lock", mailboxID)
	}

	// Disconnect if connected
	if client, exists := s.clients[mailboxID]; exists {
		client.Logout()
		delete(s.clients, mailboxID)
	}

	// Remove configuration
	delete(s.mailboxConfigs, mailboxID)
}

func (s *IMAPService) runSingleMailbox(ctx context.Context, mailboxID string, config *models.Mailbox) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "IMAPService.runSingleMailbox", telemetry.WithNewRoot())
	defer spans.Finish()
	spans.TagEntity(mailboxID)
	spans.LogObjectAsJson("mailbox", config)

	s.wg.Add(1)
	defer s.wg.Done()

	log.Printf("[%s] Starting mailbox monitoring with folders: %v", mailboxID, config.SyncFolders)

	backoff := time.Second
	maxBackoff := 2 * time.Minute
	attempts := 0

	for iteration := 0; iteration < SYNC_EMAILS_ITERATIONS_PER_ACQUIRE; iteration++ {
		// Get current mailbox state and validate pod ID
		mailbox, err := s.repositories.MailboxRepository.GetMailbox(ctx, mailboxID)
		if err != nil {
			log.Printf("[%s] Failed to get mailbox: %v", mailboxID, err)
			break
		}

		// Check if this pod still owns the lock
		if mailbox.ProcessingPodID != s.podID {
			log.Printf("[%s] Pod ID mismatch. Current: %s, Expected: %s", mailboxID, mailbox.ProcessingPodID, s.podID)
			break
		}

		// Update heartbeat and increment run count
		err = s.repositories.MailboxRepository.UpdateMailboxHeartbeat(ctx, mailboxID, s.podID)
		if err != nil {
			log.Printf("[%s] Failed to update heartbeat: %v", mailboxID, err)
			break
		}

		if err := s.processSingleMailboxIteration(ctx, mailboxID, config, &attempts, &backoff, maxBackoff); err != nil {
			// If context is cancelled, we should exit
			if errors.Is(err, context.Canceled) {
				return
			}
			// Other errors are handled within processSingleMailboxIteration
			continue
		}

		// If we reach here, wait before next iteration
		select {
		case <-time.After(30 * time.Second):
			// Continue with next iteration
		case <-ctx.Done():
			return
		}
	}

	// Release the lock after iterations complete
	s.releaseMailbox(ctx, mailboxID)
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
	imapClient, err := s.connectToIMAPServer(connectCtx, config)
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
	s.clients[mailboxID] = imapClient
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
	_, connectivityError := s.syncFolders(ctx, imapClient, mailboxID, config.SyncFolders)

	// Handle connectivity errors
	if connectivityError != nil {
		s.clientsMutex.Lock()
		delete(s.clients, mailboxID)
		s.clientsMutex.Unlock()

		err = s.repositories.MailboxRepository.UpdateConnectionStatus(ctx, mailboxID, enum.ConnectionNotActive, connectivityError.Error())
		if err != nil {
			spans.TraceError(err)
		}

		spans.LogKV("connectivityError", true)
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

	log.Printf("[%s] Connecting to IMAP server %s (Provider: %s, Security: %s, Username: %s)",
		config.ID, serverAddr, config.Provider, config.ImapSecurity, config.ImapUsername)

	// Set up connection with timeout
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	// Connect with or without TLS
	var imapClient *client.Client
	var err error

	if config.ImapSecurity == enum.EmailSecurityTLS {
		tlsConfig := &tls.Config{
			ServerName: config.ImapServer,
		}
		imapClient, err = client.DialWithDialerTLS(dialer, serverAddr, tlsConfig)
	} else {
		imapClient, err = client.DialWithDialer(dialer, serverAddr)
	}

	if err != nil {
		err := fmt.Errorf("connection error: %w", err)
		spans.TraceError(err)
		return nil, err
	}

	// Check capabilities
	imapClient.Timeout = 30 * time.Second
	caps, err := imapClient.Capability()
	if err != nil {
		imapClient.Logout()
		err := fmt.Errorf("capability error: %w", err)
		spans.TraceError(err)
		return nil, err
	}

	log.Printf("[%s] Server capabilities: %v", config.ID, caps)

	// Handle authentication based on provider
	switch config.Provider {
	case enum.EmailGoogleWorkspace:
		// For Gmail, refresh token if needed before getting access token
		err = s.googleService.RefreshTokenIfNeeded(ctx, config)
		if err != nil {
			imapClient.Logout()
			spans.TraceError(err)
			log.Printf("[%s] Failed to refresh token: %v", config.ID, err)
			return nil, fmt.Errorf("failed to refresh token: %w", err)
		}

		// Get the access token
		accessToken, err := s.googleService.GetDecryptedAccessToken(ctx, config)
		if err != nil {
			imapClient.Logout()
			spans.TraceError(err)
			log.Printf("[%s] Failed to get access token: %v", config.ID, err)
			return nil, fmt.Errorf("failed to get access token: %w", err)
		}

		log.Printf("[%s] Attempting Gmail OAuth authentication", config.ID)

		// Use XOAUTH2 authentication
		err = imapClient.Authenticate(&XOAuth2Auth{
			Username: config.ImapUsername,
			Token:    accessToken,
		})
		if err != nil {
			imapClient.Logout()
			spans.TraceError(err)
			log.Printf("[%s] Failed to authenticate with Gmail: %v", config.ID, err)
			return nil, fmt.Errorf("failed to authenticate with Gmail: %w", err)
		}

	default:
		// Default to password authentication
		log.Printf("[%s] Attempting password authentication", config.ID)
		err = imapClient.Login(config.ImapUsername, config.ImapPassword)
	}

	if err != nil {
		imapClient.Logout()
		spans.TraceError(err)
		log.Printf("[%s] Authentication failed: %v", config.ID, err)
		return nil, fmt.Errorf("login error: %w", err)
	}

	log.Printf("[%s] Successfully authenticated", config.ID)

	// Reset timeout
	imapClient.Timeout = 0

	log.Printf("[%s] Successfully connected to %s", config.ID, serverAddr)
	return imapClient, nil
}

// XOAuth2Auth implements XOAUTH2 authentication for Gmail
type XOAuth2Auth struct {
	Username string
	Token    string
}

func (a *XOAuth2Auth) Start() (string, []byte, error) {
	// Format: base64("user=" + username + "^Aauth=Bearer " + token + "^A^A")
	auth := fmt.Sprintf("user=%s\x01auth=Bearer %s\x01\x01", a.Username, a.Token)
	return "XOAUTH2", []byte(auth), nil
}

func (a *XOAuth2Auth) Next(challenge []byte) ([]byte, error) {
	// Gmail may send an empty challenge or error message
	// We should respond with an empty slice to continue the auth process
	return []byte{}, nil
}

// syncFolders processes all folders and returns information about the sync process
func (s *IMAPService) syncFolders(
	ctx context.Context,
	client *client.Client,
	mailboxID string,
	folders []string,
) (processedFolders map[string]bool, connectivityError error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "IMAPService.syncFolders")
	defer spans.Finish()
	spans.TagEntity(mailboxID)
	spans.LogObjectAsJson("folders", folders)

	processedFolders = make(map[string]bool)

	log.Printf("[%s] Starting sync for %d folders: %v", mailboxID, len(folders), folders)

	// known folders
	mailboxServerFolders, err := s.ListFolders(ctx, mailboxID)
	if err != nil {
		spans.TraceError(err)
	}
	spans.LogObjectAsJson("mailbox_server_folders", mailboxServerFolders)

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
func (s *IMAPService) processFolder(ctx context.Context, imapClient *client.Client, mailboxID, folderName string) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "IMAPService.processFolder")
	defer spans.Finish()
	spans.TagEntity(mailboxID)
	spans.TagString("folder", folderName)

	// Check for nil client
	if imapClient == nil {
		err := fmt.Errorf("IMAP client is nil")
		spans.TraceError(err)
		return err
	}

	// Select the folder
	imapClient.Timeout = 30 * time.Second
	mbox, err := imapClient.Select(folderName, false)
	imapClient.Timeout = 0
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

	// create a new sync state if it doesn't exist
	if syncState == nil {
		syncState = &models.MailboxSyncState{
			MailboxID:  mailboxID,
			FolderName: folderName,
			LastUID:    0,
		}
		err = s.repositories.MailboxSyncRepository.SaveSyncState(ctx, syncState)
		if err != nil {
			spans.TraceError(err)
			return err
		}
	}

	// We have a previous sync state, sync new messages
	log.Printf("[%s][%s] Resuming sync from UID %d", mailboxID, folderName, syncState.LastUID)
	err = s.syncNewMessagesSince(ctx, imapClient, mailboxID, folderName, syncState.LastUID, mbox.UidNext)
	if err != nil {
		err = fmt.Errorf("error syncing new messages: %w", err)
		spans.TraceError(err)
		return err
	}

	return nil
}

func (s *IMAPService) publishNewEmailEvent(ctx context.Context, event *pb.EmailReceivedIMAP) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "IMAPService.publishNewEmailEvent")
	defer spans.Finish()

	data, err := proto.Marshal(event)
	if err != nil {
		spans.TraceError(err)
		return fmt.Errorf("failed to marshal stored email: %w", err)
	}

	// Create message with headers
	msg := nats.NewMsg(enum.EventEmailInboundReceivedIMAP.String())
	msg.Data = data
	msg.Header.Set(interfaces.HEADER_TENANT, utils.GetTenantFromContext(ctx))
	msg.Header.Set(interfaces.HEADER_USERID, utils.GetUserIdFromContext(ctx))

	// Publish to the stored subject
	_, err = s.natsConn.JS.PublishMsg(msg)
	if err != nil {
		spans.TraceError(err)
		return fmt.Errorf("failed to publish stored email: %w", err)
	}

	return nil
}

// syncNewMessagesSince syncs messages with UID greater than lastUID
func (s *IMAPService) syncNewMessagesSince(
	ctx context.Context,
	imapClient *client.Client,
	mailboxID, folderName string,
	lastUID uint32,
	mboxUIDNext uint32,
) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "IMAPService.syncNewMessagesSince")
	defer spans.Finish()
	spans.TagEntity(mailboxID)
	spans.TagString("folder", folderName)
	spans.LogKV("last_uid", lastUID, "mbox_uid_next", mboxUIDNext)

	collectedUIDs := make([]uint32, 0)
	startUID := lastUID + 1
	size := SYNC_EMAILS_BATCH_SIZE_PER_ITERATION

	for startUID < mboxUIDNext {
		stopUID := startUID + uint32(size) - 1
		if stopUID <= startUID {
			stopUID = startUID + 1
		}
		if stopUID > mboxUIDNext {
			stopUID = 0
		}

		// Create search criteria for UIDs greater than lastUID
		criteria := imap.NewSearchCriteria()
		uidRange := new(imap.SeqSet)
		uidRange.AddRange(startUID, stopUID)
		criteria.Uid = uidRange

		// Set timeout
		imapClient.Timeout = 30 * time.Second
		imapUids, err := imapClient.UidSearch(criteria)
		if err != nil {
			err = fmt.Errorf("error searching for new messages: %w", err)
			spans.TraceError(err)
			return err
		}
		imapClient.Timeout = 0

		collectedUIDs = append(collectedUIDs, imapUids...)

		if stopUID == 0 || len(collectedUIDs) >= size {
			break
		}
		startUID = stopUID + 1
	}

	spans.LogKV("original_uids_count", len(collectedUIDs))

	// Filter out any UIDs that are less than or equal to our last processed UID
	filteredUIDs := make([]uint32, 0, len(collectedUIDs))
	for _, uid := range collectedUIDs {
		if uid > lastUID {
			filteredUIDs = append(filteredUIDs, uid)
		} else {
			log.Printf("[%s][%s] Skipping already processed UID %d (lastUID: %d)", mailboxID, folderName, uid, lastUID)
		}
	}

	spans.LogKV("filtered_uids_count", len(filteredUIDs))

	if len(filteredUIDs) == 0 {
		log.Printf("[%s][%s] No new messages since UID %d", mailboxID, folderName, lastUID)
		return nil
	}

	log.Printf("[%s][%s] Found %d new messages since UID %d", mailboxID, folderName, len(filteredUIDs), lastUID)

	// Create sequence set
	seqSet := new(imap.SeqSet)
	for _, uid := range filteredUIDs {
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
	imapClient.Timeout = 60 * time.Second

	// Start fetch
	go func() {
		done <- imapClient.UidFetch(seqSet, items, messages)
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
		event := &pb.EmailReceivedIMAP{
			MailboxId:   mailboxID,
			Folder:      folderName,
			ImapSeqNum:  msg.SeqNum,
			ImapUid:     msg.Uid,
			InitialSync: false,
		}
		err := s.publishNewEmailEvent(ctx, event)
		if err != nil {
			spans.TraceError(err)
			return err
		}
	}

	// Reset timeout
	imapClient.Timeout = 0

	// Check for fetch errors
	err := <-done
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

func (s *IMAPService) AcceptMailboxForSync(ctx context.Context, mailbox *models.Mailbox) bool {
	spans, _ := telemetry.StartServiceSpan(ctx, "IMAPService.AcceptMailboxForSync")
	defer spans.Finish()
	spans.TagEntity(mailbox.ID)

	if !mailbox.InboundEnabled {
		spans.LogKV("result.accepted", false)
		spans.LogKV("reason", "inbound_disabled")
		return false
	}

	if mailbox.ProvisionStatus != models.MailboxStatusProvisioned {
		spans.LogKV("result.accepted", false)
		spans.LogKV("reason", "provision_status_not_provisioned")
		return false
	}

	if mailbox.Provider != enum.EmailMailstack && mailbox.Provider != enum.EmailGoogleWorkspace {
		spans.LogKV("result.accepted", false)
		spans.LogKV("reason", "unsupported_provider")
		return false
	}

	spans.LogKV("result.accepted", true)
	return true
}

// ListFolders returns a list of all available folders in the mailbox
func (s *IMAPService) ListFolders(ctx context.Context, mailboxID string) ([]string, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "IMAPService.ListFolders")
	defer spans.Finish()
	spans.TagString("mailbox_id", mailboxID)

	// Get connected client
	imapClient, err := s.getConnectedClient(ctx, mailboxID)
	if err != nil {
		spans.TraceError(err)
		return nil, fmt.Errorf("failed to get IMAP client: %w", err)
	}

	// List all mailboxes
	mailboxes := make(chan *imap.MailboxInfo, 10)
	done := make(chan error, 1)

	imapClient.Timeout = 30 * time.Second
	go func() {
		done <- imapClient.List("", "*", mailboxes)
	}()

	var folders []string
	for m := range mailboxes {
		folders = append(folders, m.Name)
		log.Printf("[%s] Found folder: %s (Attributes: %v)", mailboxID, m.Name, m.Attributes)
	}

	if err := <-done; err != nil {
		spans.TraceError(err)
		return nil, fmt.Errorf("error listing folders: %w", err)
	}
	imapClient.Timeout = 0

	log.Printf("[%s] Found %d folders", mailboxID, len(folders))
	return folders, nil
}
