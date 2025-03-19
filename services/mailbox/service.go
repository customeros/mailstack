package mailbox

import (
	"context"
	"fmt"
	"strings"

	"github.com/customeros/mailsherpa/mailvalidate"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"

	"github.com/customeros/mailstack/api/graphql/graphql_model"
	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/tracing"
)

type mailboxService struct {
	repositories *repository.Repositories
}

func NewMailboxService(repos *repository.Repositories) interfaces.MailboxService {
	return &mailboxService{
		repositories: repos,
	}
}

func (s *mailboxService) EnrollMailbox(ctx context.Context, mailbox *graphql_model.MailboxInput) (*models.Mailbox, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "mailboxService.CreateMailbox")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	// validate input
	err := validateMailboxInput(mailbox)
	if err != nil {
		return nil, err
	}

	// validate mailbox does not exist

	// save mailbox

	// determine if we should sync
	return nil, nil
}

func validateMailboxInput(input *graphql_model.MailboxInput) error {
	var validationErrors []string

	if input == nil {
		return errors.New("mailbox input cannot be nil")
	}

	// Validate email address
	validation := mailvalidate.ValidateEmailSyntax(input.EmailAddress)
	if !validation.IsValid {
		validationErrors = append(validationErrors, "Email address is not valid")
	}
	if validation.IsRoleAccount {
		validationErrors = append(validationErrors, "Email user cannot be role account")
	}
	if validation.IsSystemGenerated {
		validationErrors = append(validationErrors, "Invalid email user")
	}
	if validation.IsFreeAccount {
		validationErrors = append(validationErrors, "Free accounts are not supported")
	}
	input.EmailAddress = validation.CleanEmail

	// Validate IMAP configuration if provided
	if input.ImapConfig != nil {
		if input.ImapConfig.ImapUsername == nil {
			validationErrors = append(validationErrors, "IMAP username is required when IMAP config is provided")
		}
		if input.ImapConfig.ImapPassword == nil {
			validationErrors = append(validationErrors, "IMAP password is required when IMAP config is provided")
		}
	}

	// Validate SMTP configuration if provided
	if input.SMTPConfig != nil {
		if input.SMTPConfig.SMTPUsername == nil {
			validationErrors = append(validationErrors, "SMTP username is required when SMTP config is provided")
		}
		if input.SMTPConfig.SMTPPassword == nil {
			validationErrors = append(validationErrors, "SMTP password is required when SMTP config is provided")
		}
	}

	// Set default values for providers
	switch input.Provider {
	case enum.EmailMailstack:
		setTrue := true
		inbox := "INBOX"
		sent := "Sent"
		spam := "Spam"
		server := "mail.hostedemail.com"
		imapPort := 993
		smtpPort := 587
		security := enum.EmailSecurityTLS
		input.InboundEnabled = &setTrue
		input.SyncFolders = []*string{&inbox, &sent, &spam}
		input.SMTPConfig.SMTPServer = &server
		input.ImapConfig.ImapServer = &server
		input.SMTPConfig.SMTPPort = &smtpPort
		input.ImapConfig.ImapPort = &imapPort
		input.SMTPConfig.SMTPSecurity = &security
		input.ImapConfig.ImapSecurity = &security

	case enum.EmailGeneric:
		if input.SyncFolders == nil || len(input.SyncFolders) == 0 {
			validationErrors = append(validationErrors, "syncFolders must be specified for generic provider")
		}
		// TODO validate full imap/smtp inputs
	}

	if input.SenderID != nil {
		setTrue := true
		input.OutboundEnabled = &setTrue
	}

	// Check if there are any validation errors
	if len(validationErrors) > 0 {
		return fmt.Errorf("validation failed: %s", strings.Join(validationErrors, ", "))
	}

	return nil
}
