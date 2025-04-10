package internal

import (
	"context"
	"fmt"
	"github.com/customeros/mailstack/internal/telemetry"
	"log"

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

	// Add mailboxes to IMAP service
	for _, mailbox := range mailboxes {
		if mailbox.ProvisionStatus != models.MailboxStatusProvisioned {
			continue
		}
		if err = s.IMAPService.AddMailbox(ctx, mailbox); err != nil {
			spans.TraceError(err)
			return fmt.Errorf("failed to add mailbox %s: %w", mailbox.ID, err)
		}
	}

	log.Printf("Successfully initialized %d mailboxes", len(mailboxes))
	return nil
}
