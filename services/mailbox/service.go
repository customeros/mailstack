package mailbox

import (
	"context"
	"fmt"
	"strings"

	"github.com/customeros/mailsherpa/mailvalidate"
	"github.com/opentracing/opentracing-go"
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

	err := utils.ValidateTenant(ctx)
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}
	err = utils.ValidateUserId(ctx)
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}

	// Create a CreateMailboxRequest from the existing mailbox
	request := interfaces.CreateMailboxRequest{
		Domain:         mailbox.MailboxDomain,
		Username:       mailbox.MailboxUser,
		Password:       mailbox.ImapPassword,
		UserId:         utils.GetUserIdFromContext(ctx),
		ForwardingTo:   strings.Split(mailbox.ForwardingTo, ","),
		WebmailEnabled: mailbox.WebmailEnabled,
	}

	// Prepare mailbox using common method
	provider := enum.EmailMailstack
	if provider == "" {
		provider = enum.EmailGeneric
	}
	preparedMailbox, err := s.prepareMailboxForSave(ctx, provider, strings.ToLower(mailbox.EmailAddress), request)
	if err != nil {
		return nil, err
	}

	// validate mailbox does not exist
	err = s.verifyMailboxNotExists(ctx, span, preparedMailbox.EmailAddress)
	if err != nil {
		return nil, err
	}

	// validate input
	err = s.validateMailboxInput(*preparedMailbox)
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}

	// save mailbox
	mailboxID, err := s.repositories.MailboxRepository.SaveMailbox(ctx, *preparedMailbox)
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}
	if mailboxID == "" {
		err = errors.New("unable to create mailbox")
		tracing.TraceErr(span, err)
		return nil, err
	}

	// mark as provisioned
	err = s.repositories.MailboxRepository.UpdateProvisionStatus(ctx, mailboxID, models.MailboxStatusProvisioned)
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}

	preparedMailbox.ID = mailboxID

	err = s.addToIMAP(ctx, mailboxID)
	if err != nil {
		tracing.TraceErr(span, err)
		return preparedMailbox, err
	}

	return preparedMailbox, nil
}

func (s *mailboxService) addToIMAP(ctx context.Context, mailboxID string) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "mailboxService.addToIMAP")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	mailbox, err := s.repositories.MailboxRepository.GetMailbox(ctx, mailboxID)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	if mailbox == nil {
		return errors.New("mailbox not found")
	}

	// determine if we should sync
	if mailbox.Provider == enum.EmailMailstack && mailbox.InboundEnabled && mailbox.ProvisionStatus == models.MailboxStatusProvisioned {
		return s.imapService.AddMailbox(ctx, mailbox)
	}

	return nil
}

func (s *mailboxService) prepareMailboxForSave(ctx context.Context, provider enum.EmailProvider, emailAddress string, request interfaces.CreateMailboxRequest) (*models.Mailbox, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "mailboxService.prepareMailboxForSave")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	tenant := utils.GetTenantFromContext(ctx)
	if tenant == "" {
		err := errors.New("Tenant is nil")
		tracing.TraceErr(span, err)
		return nil, err
	}

	// Set default values based on provider
	var syncFolders []string
	var imapPort, smtpPort int
	var imapServer, smtpServer string
	var imapSecurity, smtpSecurity enum.EmailSecurity

	// For now, we only support mailstack provider
	syncFolders = []string{models.MAILBOX_INBOX, models.MAILBOX_SENT, models.MAILBOX_SPAM}
	imapPort = models.MAILBOX_IMAP_PORT
	smtpPort = models.MAILBOX_SMTP_PORT
	imapServer = models.MAILBOX_IMAP_SERVER
	smtpServer = models.MAILBOX_SMTP_SERVER
	imapSecurity = models.MAILBOX_IMAP_SECURITY
	smtpSecurity = models.MAILBOX_SMTP_SECURITY

	mailbox := models.Mailbox{
		ID:              utils.GenerateNanoIDWithPrefix("mbox", 16),
		Tenant:          tenant,
		MailboxDomain:   strings.ToLower(request.Domain),
		EmailAddress:    emailAddress,
		MailboxUser:     request.Username,
		UserID:          request.UserId,
		Provider:        provider,
		SyncFolders:     syncFolders,
		InboundEnabled:  true,
		OutboundEnabled: true,
		SenderID:        request.SenderID,

		ImapServer:   imapServer,
		ImapPort:     imapPort,
		ImapUsername: strings.ToLower(emailAddress),
		ImapPassword: request.Password,
		ImapSecurity: imapSecurity,

		SmtpServer:   smtpServer,
		SmtpPort:     smtpPort,
		SmtpUsername: strings.ToLower(emailAddress),
		SmtpPassword: request.Password,
		SmtpSecurity: smtpSecurity,

		ForwardingTo:    strings.Join(request.ForwardingTo, ","),
		WebmailEnabled:  request.WebmailEnabled,
		ProvisionStatus: models.MailboxStatusPendingProvisioning,
		RampUpRate:      3,
		RampUpMax:       40,
		RampUpCurrent:   3,
	}

	return &mailbox, nil
}

func (s *mailboxService) validateMailboxInput(input models.Mailbox) error {
	var validationErrors []string

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

	// Validate provider-specific requirements
	switch input.Provider {
	case enum.EmailGeneric:
		if len(input.SyncFolders) == 0 {
			validationErrors = append(validationErrors, "syncFolders must be specified for generic provider")
		}
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

func (s *mailboxService) ConfigureMailbox(ctx context.Context, mailboxID string) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "MailboxService.ConfigureMailbox")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)
	tracing.TagEntity(span, mailboxID)

	tenant := utils.GetTenantFromContext(ctx)

	// Get the mailbox from the repository
	mailbox, err := s.repositories.MailboxRepository.GetMailbox(ctx, mailboxID)
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
	err = s.repositories.MailboxRepository.UpdateProvisionStatus(ctx, mailboxID, models.MailboxStatusProvisioned)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "failed to update mailbox status"))
		return errors.Wrap(err, "failed to update mailbox status")
	}

	err = s.addToIMAP(ctx, mailboxID)
	if err != nil {
		tracing.TraceErr(span, err)
		return nil
	}

	return nil
}

func (s *mailboxService) CreateMailbox(ctx context.Context, request interfaces.CreateMailboxRequest) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "MailboxService.CreateMailbox")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)
	tracing.LogObjectAsJson(span, "request", request)

	if !request.IgnoreDomainOwnership {
		if err := s.validateCreateMailboxRequest(ctx, span, request.Domain); err != nil {
			tracing.TraceErr(span, errors.Wrap(err, "cannot validate MailboxRequest"))
			return err
		}
	}

	mailboxEmailAddress := strings.ToLower(request.Username + "@" + request.Domain)

	// Verify mailbox doesn't exist
	if err := s.verifyMailboxNotExists(ctx, span, mailboxEmailAddress); err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "failed to verify mailbox does not exist"))
		return err
	}

	// Prepare mailbox using common method
	mailbox, err := s.prepareMailboxForSave(ctx, enum.EmailMailstack, mailboxEmailAddress, request)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "Error preparing mailbox"))
		return err
	}

	// Save mailbox
	mailboxID, err := s.repositories.MailboxRepository.SaveMailbox(ctx, *mailbox)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "Error saving mailbox"))
		return err
	}
	if mailboxID == "" {
		return errors.New("failed to save mailbox")
	}
	tracing.TagEntity(span, mailboxID)
	return nil
}

func (s *mailboxService) validateCreateMailboxRequest(ctx context.Context, span opentracing.Span, domain string) error {
	if err := utils.ValidateTenant(ctx); err != nil {
		tracing.TraceErr(span, err)
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
