package internal

import (
	"context"
	"fmt"
	"github.com/customeros/mailstack/internal/enum"
	"log"

	"github.com/customeros/mailstack/internal/telemetry"

	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/services"
)

// InitMailboxes initializes all mailbox connections from configuration
func InitMailboxes(s *services.Services, r *repository.Repositories) error {
	log.Println("Initializing mailbox connections...")

	spans, ctx := telemetry.StartSpan(context.Background(), "InitMailboxes")
	defer spans.Finish()

	// get mailboxes from database
	mailboxes, err := r.MailboxRepository.GetMailboxes(ctx)
	if err != nil {
		return err
	}

	skippedMailboxesByStatus := make(map[string][]string)
	skippedMailboxesByProvider := make(map[string][]string)
	// Add mailboxes to IMAP service
	for _, mailbox := range mailboxes {
		if mailbox.Provider != enum.EmailMailstack {
			if _, exists := skippedMailboxesByProvider[mailbox.Tenant]; !exists {
				skippedMailboxesByProvider[mailbox.Tenant] = make([]string, 0)
			}
			skippedMailboxesByProvider[mailbox.Tenant] = append(skippedMailboxesByProvider[mailbox.Tenant], mailbox.EmailAddress)
			continue
		}
		if mailbox.ProvisionStatus != models.MailboxStatusProvisioned {
			if _, exists := skippedMailboxesByStatus[mailbox.Tenant]; !exists {
				skippedMailboxesByStatus[mailbox.Tenant] = make([]string, 0)
			}
			skippedMailboxesByStatus[mailbox.Tenant] = append(skippedMailboxesByStatus[mailbox.Tenant], mailbox.EmailAddress)
			continue
		}
		if err = s.IMAPService.AddMailbox(ctx, mailbox); err != nil {
			spans.TraceError(err)
			return fmt.Errorf("failed to add mailbox %s: %w", mailbox.ID, err)
		}
	}

	spans.LogObjectAsJson("skipped_mailboxes_by_status", skippedMailboxesByStatus)
	spans.LogObjectAsJson("skipped_mailboxes_by_provider", skippedMailboxesByProvider)
	log.Printf("Successfully initialized %d mailboxes", len(mailboxes))
	return nil
}
