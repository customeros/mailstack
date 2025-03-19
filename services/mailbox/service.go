package mailbox

import (
	"context"
	"fmt"
	"strings"

	"github.com/customeros/mailsherpa/mailvalidate"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"gorm.io/gorm"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"
)

type mailboxService struct {
	repositories *repository.Repositories
	imapService  interfaces.IMAPService
}

func NewMailboxService(repos *repository.Repositories, imap interfaces.IMAPService) interfaces.MailboxService {
	return &mailboxService{
		repositories: repos,
		imapService:  imap,
	}
}

var ErrMailboxExists = errors.New("Mailbox already exists")

func (s *mailboxService) EnrollMailbox(ctx context.Context, mailbox *models.Mailbox) (*models.Mailbox, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "mailboxService.CreateMailbox")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	tenant := utils.GetTenantFromContext(ctx)
	if tenant == "" {
		err := errors.New("Tenant is nil")
		tracing.TraceErr(span, err)
		return nil, err
	}
	userId := utils.GetUserIdFromContext(ctx)
	if userId == "" {
		err := errors.New("UserId is nil")
		tracing.TraceErr(span, err)
		return nil, err
	}

	mailbox.Tenant = tenant
	mailbox.UserID = userId

	// validate input
	err := validateMailboxInput(mailbox)
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}

	// validate mailbox does not exist
	mboxCheck, err := s.repositories.MailboxRepository.GetMailboxByEmailAddress(ctx, mailbox.EmailAddress)
	if err != nil && err != gorm.ErrRecordNotFound {
		tracing.TraceErr(span, err)
		return nil, err
	}
	if mboxCheck != nil {
		tracing.TraceErr(span, ErrMailboxExists)
		return nil, ErrMailboxExists
	}

	// save mailbox
	mailboxId, err := s.repositories.MailboxRepository.SaveMailbox(ctx, *mailbox)
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}
	if mailboxId == "" {
		err = errors.New("unable to create mailbox")
		tracing.TraceErr(span, err)
		return nil, err
	}

	mailbox.ID = mailboxId

	// determine if we should sync
	if mailbox.Provider == enum.EmailMailstack && mailbox.InboundEnabled {
		s.imapService.AddMailbox(ctx, mailbox)
	}

	return mailbox, nil
}

func validateMailboxInput(input *models.Mailbox) error {
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

	// Set default values for providers
	switch input.Provider {
	case enum.EmailMailstack:
		inbox := "INBOX"
		sent := "Sent"
		spam := "Spam"
		server := "mail.hostedemail.com"
		imapPort := 993
		smtpPort := 587
		security := enum.EmailSecurityTLS
		input.InboundEnabled = true
		input.SyncFolders = []string{inbox, sent, spam}
		input.SmtpServer = server
		input.ImapServer = server
		input.SmtpPort = smtpPort
		input.ImapPort = imapPort
		input.SmtpSecurity = security
		input.ImapSecurity = security

	case enum.EmailGeneric:
		if input.SyncFolders == nil || len(input.SyncFolders) == 0 {
			validationErrors = append(validationErrors, "syncFolders must be specified for generic provider")
		}
		// TODO validate full imap/smtp inputs
	}

	// Validate IMAP configuration if provided
	if input.ImapPassword != "" {
		if input.ImapUsername == "" {
			validationErrors = append(validationErrors, "IMAP username is required when IMAP config is provided")
		}
		if input.ImapServer == "" {
			validationErrors = append(validationErrors, "IMAP server is required when IMAP config is provided")
		}
		if input.ImapUsername == "" {
			validationErrors = append(validationErrors, "IMAP username is required when IMAP config is provided")
		}
		if input.ImapSecurity == "" {
			validationErrors = append(validationErrors, "IMAP security is required when IMAP config is provided")
		}
	}

	// Validate SMTP configuration if provided
	if input.SmtpPassword != "" {
		if input.SmtpUsername == "" {
			validationErrors = append(validationErrors, "SMTP username is required when SMTP config is provided")
		}
		if input.SmtpServer == "" {
			validationErrors = append(validationErrors, "SMTP server is required when SMTP config is provided")
		}
		if input.SmtpUsername == "" {
			validationErrors = append(validationErrors, "SMTP username is required when SMTP config is provided")
		}
		if input.SmtpSecurity == "" {
			validationErrors = append(validationErrors, "SMTP security is required when SMTP config is provided")
		}
	}

	if input.SenderID != "" {
		input.OutboundEnabled = true
	}

	// Check if there are any validation errors
	if len(validationErrors) > 0 {
		return fmt.Errorf("validation failed: %s", strings.Join(validationErrors, ", "))
	}

	return nil
}
