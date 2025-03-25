package mailbox

import (
	"context"
	"fmt"
	"strings"

	"github.com/customeros/mailsherpa/mailvalidate"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
	"github.com/pkg/errors"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"

	internalerrors "github.com/customeros/mailstack/internal/errors"
)

const TEST_MAILBOX_DOMAIN = "testcustomeros.com"

type mailboxService struct {
	repositories   *repository.Repositories
	imapService    interfaces.IMAPService
	openSrsService interfaces.OpenSrsService
}

func NewMailboxService(repos *repository.Repositories, imap interfaces.IMAPService, openSrs interfaces.OpenSrsService) interfaces.MailboxService {
	return &mailboxService{
		repositories:   repos,
		imapService:    imap,
		openSrsService: openSrs,
	}
}

func (s *mailboxService) EnrollMailbox(ctx context.Context, mailbox *models.Mailbox) (*models.Mailbox, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "mailboxService.EnrollMailbox")
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

	// Set default status as provisions for backward compatibility of the API
	mailbox.ProvisionStatus = models.MailboxStatusProvisioned

	// validate input
	err := validateMailboxInput(mailbox)
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}

	// validate mailbox does not exist
	err = s.verifyMailboxNotExists(ctx, span, mailbox.EmailAddress)
	if err != nil {
		return nil, err
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
	if mailbox.Provider == enum.EmailMailstack && mailbox.InboundEnabled && mailbox.ProvisionStatus == models.MailboxStatusProvisioned {
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
		imapPort := models.MAILBOX_IMAP_PORT
		smtpPort := models.MAILBOX_SMTP_PORT
		input.InboundEnabled = true
		input.SyncFolders = []string{inbox, sent, spam}
		input.SmtpServer = models.MAILBOX_SMTP_SERVER
		input.ImapServer = models.MAILBOX_IMAP_SERVER
		input.SmtpPort = smtpPort
		input.ImapPort = imapPort
		input.SmtpSecurity = models.MAILBOX_SMTP_SECURITY
		input.ImapSecurity = models.MAILBOX_IMAP_SECURITY

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

func (s *mailboxService) RampUpMailboxes(ctx context.Context) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "MailboxService.RampUpMailboxes")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	mailboxes, err := s.repositories.MailboxRepository.GetForRampUp(ctx)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	span.LogKV("mailboxes.count", len(mailboxes))

	for _, mailbox := range mailboxes {
		innerCtx := utils.WithTenantContext(ctx, mailbox.Tenant)
		err := s.rampUpMailbox(innerCtx, mailbox)
		if err != nil {
			tracing.TraceErr(span, err)
			// Continue processing other mailboxes even if one fails
			continue
		}
	}

	return nil
}

func (s *mailboxService) rampUpMailbox(ctx context.Context, mailbox *models.Mailbox) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "MailboxService.rampUpMailbox")
	defer span.Finish()
	tracing.TagComponentCronJob(span)

	for {
		if mailbox.RampUpCurrent >= mailbox.RampUpMax {
			break
		}

		if mailbox.LastRampUpAt.After(utils.StartOfDayInUTC(utils.Now())) {
			break
		}

		mailbox.RampUpCurrent = mailbox.RampUpCurrent + mailbox.RampUpRate

		if mailbox.RampUpCurrent > mailbox.RampUpMax {
			mailbox.RampUpCurrent = mailbox.RampUpMax
		}

		mailbox.LastRampUpAt = mailbox.LastRampUpAt.AddDate(0, 0, 1)

		err := s.repositories.MailboxRepository.UpdateRampUpFields(ctx, mailbox)
		if err != nil {
			tracing.TraceErr(span, err)
			return err
		}
	}

	return nil
}

func (s *mailboxService) ConfigureMailbox(ctx context.Context, mailboxId string) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "MailboxService.ConfigureMailbox")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)
	tracing.TagEntity(span, mailboxId)

	tenant := utils.GetTenantFromContext(ctx)

	// Get the mailbox from the repository
	mailbox, err := s.repositories.MailboxRepository.GetMailbox(ctx, mailboxId)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "failed to get mailbox"))
		return errors.Wrap(err, "failed to get mailbox")
	}
	if mailbox == nil {
		return internalerrors.ErrMailboxNotFound
	}

	if mailbox.Provider != enum.EmailMailstack {
		return errors.New("mailbox provider is not supported for configuration")
	}

	// Verify tenant ownership
	if mailbox.Tenant != tenant {
		return internalerrors.ErrMailboxNotOwnedByTenant
	}

	// Parse forwarding addresses
	forwardingTo := []string{}
	if mailbox.ForwardingTo != "" {
		forwardingTo = strings.Split(mailbox.ForwardingTo, ",")
	}

	// Configure mailbox with OpenSRS
	err = s.openSrsService.SetupMailbox(ctx, tenant, mailbox.EmailAddress, mailbox.SmtpPassword, forwardingTo, mailbox.WebmailEnabled)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "failed to configure mailbox with OpenSRS"))

		dbErr := s.repositories.MailboxRepository.ConfigureAttempt(ctx, mailbox.ID)
		if dbErr != nil {
			tracing.TraceErr(span, dbErr)
			return dbErr
		}

		return errors.Wrap(err, "failed to configure mailbox with OpenSRS")
	}

	// Update mailbox status to provisioned
	err = s.repositories.MailboxRepository.UpdateProvisionStatus(ctx, mailboxId, models.MailboxStatusProvisioned)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "failed to update mailbox status"))
		return errors.Wrap(err, "failed to update mailbox status")
	}

	return nil
}

// CreateMailbox creates a new mailbox
// TODO for now we only support mailstack
func (s *mailboxService) CreateMailbox(ctx context.Context, request interfaces.CreateMailboxRequest) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "MailboxService.CreateMailbox")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)
	span.LogFields(
		log.String("userId", request.UserId),
		log.String("domain", request.Domain),
		log.String("username", request.Username),
		log.Bool("webmailEnabled", request.WebmailEnabled),
		log.Object("forwardingTo", request.ForwardingTo),
	)

	if !request.IgnoreDomainOwnership {
		if err := s.validateRequest(ctx, span, request.Domain); err != nil {
			tracing.TraceErr(span, errors.Wrap(err, "cannot vaildate MailboxRequest"))
			return err
		}
	}

	mailboxEmail := request.Username + "@" + request.Domain

	// Verify mailbox doesn't exist
	if err := s.verifyMailboxNotExists(ctx, span, mailboxEmail); err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "failed to verify mailbox does not exist"))
		return err
	}

	// Save mailbox
	if err := s.createMailstackMailbox(ctx, request, mailboxEmail, request.UserId); err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "failed to save mailbox"))
		return err
	}

	return nil
}

func (s *mailboxService) validateRequest(ctx context.Context, span opentracing.Span, domain string) error {
	if err := utils.ValidateTenant(ctx); err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "Error validating tenant"))
		return err
	}

	tenant := utils.GetTenantFromContext(ctx)
	if domain != TEST_MAILBOX_DOMAIN {
		domainBelongsToTenant, err := s.repositories.DomainRepository.CheckDomainOwnership(ctx, tenant, domain)
		if err != nil {
			tracing.TraceErr(span, errors.Wrap(err, "Error checking domain"))
			return errors.Wrap(err, "Error checking domain")
		}
		if !domainBelongsToTenant {
			tracing.TraceErr(span, errors.Wrap(internalerrors.ErrDomainNotFound, "domain does not belong to tenant"))
			return internalerrors.ErrDomainNotFound
		}
	}
	return nil
}

func (s *mailboxService) verifyMailboxNotExists(ctx context.Context, span opentracing.Span, mailboxEmail string) error {
	mboxCheck, err := s.repositories.MailboxRepository.GetMailboxByEmailAddressCrossTenant(ctx, mailboxEmail)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}
	if mboxCheck != nil {
		tracing.TraceErr(span, internalerrors.ErrMailboxExists)
		return internalerrors.ErrMailboxExists
	}
	return nil
}

func (s *mailboxService) createMailstackMailbox(ctx context.Context, request interfaces.CreateMailboxRequest, mailboxEmail, userId string) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "MailboxService.createMailstackMailbox")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	tenant := utils.GetTenantFromContext(ctx)
	mailbox := models.Mailbox{
		ID:              utils.GenerateNanoIDWithPrefix("mbox", 16),
		Tenant:          tenant,
		MailboxDomain:   request.Domain,
		EmailAddress:    mailboxEmail,
		MailboxUser:     request.Username,
		UserID:          userId,
		Provider:        enum.EmailMailstack,
		SyncFolders:     []string{models.MAILBOX_INBOX, models.MAILBOX_SENT, models.MAILBOX_SPAM},
		InboundEnabled:  true,
		OutboundEnabled: true,

		ImapServer:   models.MAILBOX_IMAP_SERVER,
		ImapPort:     models.MAILBOX_IMAP_PORT,
		ImapUsername: mailboxEmail,
		ImapPassword: request.Password,
		ImapSecurity: models.MAILBOX_IMAP_SECURITY,

		SmtpServer:   models.MAILBOX_SMTP_SERVER,
		SmtpPort:     models.MAILBOX_SMTP_PORT,
		SmtpUsername: mailboxEmail,
		SmtpPassword: request.Password,
		SmtpSecurity: models.MAILBOX_SMTP_SECURITY,

		ForwardingTo:    strings.Join(request.ForwardingTo, ","),
		WebmailEnabled:  request.WebmailEnabled,
		ProvisionStatus: models.MailboxStatusPendingProvisioning,
		RampUpRate:      3,
		RampUpMax:       40,
		RampUpCurrent:   3,
	}
	mailboxId, err := s.repositories.MailboxRepository.SaveMailbox(ctx, mailbox)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "Error saving mailbox"))
		return err
	}
	if mailboxId == "" {
		return errors.New("failed to save mailbox")
	}
	tracing.TagEntity(span, mailboxId)
	return nil
}

// GetMailboxes returns all mailboxes for a given domain
// If domain is empty, it returns all mailboxes for the tenant
func (s *mailboxService) GetMailboxes(ctx context.Context, provider enum.EmailProvider, domain, userId string) ([]*models.Mailbox, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "MailboxService.GetMailboxes")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)
	span.LogKV("domain", domain, "userId", userId, "provider", provider)

	// Get mailboxes with filters
	mailboxRecords, err := s.repositories.MailboxRepository.GetAllWithFilters(ctx, provider, domain, userId)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "Error retrieving mailboxes"))
		return nil, err
	}
	span.LogKV("result.count", len(mailboxRecords))
	return mailboxRecords, nil
}

func (s *mailboxService) GetMailboxByEmailAddress(ctx context.Context, emailAddress string) (*models.Mailbox, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "MailboxService.GetMailboxByEmailAddress")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)
	span.LogKV("emailAddress", emailAddress)

	mailboxRecord, err := s.repositories.MailboxRepository.GetMailboxByEmailAddress(ctx, emailAddress)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "Error retrieving mailbox"))
		return nil, err
	}
	return mailboxRecord, nil
}
