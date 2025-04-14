package mailbox

import (
	"context"
	"fmt"
	"strings"

	"github.com/customeros/mailsherpa/mailvalidate"
	"github.com/pkg/errors"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/telemetry"
	"github.com/customeros/mailstack/internal/utils"

	internalerrors "github.com/customeros/mailstack/internal/errors"
)

const TEST_MAILBOX_DOMAIN = "testcustomeros.com"

type mailboxService struct {
	repositories   *repository.Repositories
	openSrsService interfaces.OpenSrsService
}

func NewMailboxService(repos *repository.Repositories, openSrs interfaces.OpenSrsService) interfaces.MailboxService {
	return &mailboxService{
		repositories:   repos,
		openSrsService: openSrs,
	}
}

func (s *mailboxService) EnrollMailbox(ctx context.Context, mailbox *models.Mailbox) (*models.Mailbox, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "mailboxService.EnrollMailbox")
	defer spans.Finish()

	err := utils.ValidateTenant(ctx)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}
	err = utils.ValidateUserId(ctx)
	if err != nil {
		spans.TraceError(err)
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
		SenderID:       mailbox.SenderID,
	}

	oauthData := interfaces.OauthMailboxRequest{
		OAuthAccessToken:  mailbox.OAuthAccessToken,
		OAuthRefreshToken: mailbox.OAuthRefreshToken,
		OAuthTokenId:      mailbox.OAuthTokenId,
		OAuthScope:        mailbox.OAuthScope,
		OAuthTokenExpiry:  mailbox.OAuthTokenExpiry,
	}

	// Prepare mailbox using common method
	provider := mailbox.Provider
	if provider == "" {
		provider = enum.EmailGeneric
	}
	preparedMailbox, err := s.prepareMailboxForSave(ctx, provider, strings.ToLower(mailbox.EmailAddress), request, &oauthData)
	if err != nil {
		return nil, err
	}

	// validate mailbox does not exist
	err = s.verifyMailboxNotExists(ctx, spans, preparedMailbox.EmailAddress)
	if err != nil {
		return nil, err
	}

	// validate input
	err = s.validateMailboxInput(*preparedMailbox)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	// save mailbox
	mailboxID, err := s.repositories.MailboxRepository.SaveMailbox(ctx, *preparedMailbox)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}
	if mailboxID == "" {
		err = errors.New("unable to create mailbox")
		spans.TraceError(err)
		return nil, err
	}

	// mark as provisioned
	err = s.repositories.MailboxRepository.UpdateProvisionStatus(ctx, mailboxID, models.MailboxStatusProvisioned)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	preparedMailbox.ID = mailboxID

	return preparedMailbox, nil
}

func (s *mailboxService) prepareMailboxForSave(ctx context.Context, provider enum.EmailProvider, emailAddress string, request interfaces.CreateMailboxRequest, oauth *interfaces.OauthMailboxRequest) (*models.Mailbox, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "mailboxService.prepareMailboxForSave")
	defer spans.Finish()

	tenant := utils.GetTenantFromContext(ctx)
	if tenant == "" {
		err := errors.New("Tenant is nil")
		spans.TraceError(err)
		return nil, err
	}

	// Set default values based on provider
	var syncFolders []string
	var imapPort, smtpPort int
	var imapServer, smtpServer string
	var imapSecurity, smtpSecurity enum.EmailSecurity

	// Configure based on provider
	switch provider {
	case enum.EmailGoogleWorkspace:
		syncFolders = []string{models.MAILBOX_GOOGLE_INBOX, models.MAILBOX_GOOGLE_SENT, models.MAILBOX_GOOGLE_SPAM}
		imapServer = models.MAILBOX_GOOGLE_IMAP_SERVER
		imapPort = models.MAILBOX_GOOGLE_IMAP_PORT
		imapSecurity = models.MAILBOX_GOOGLE_IMAP_SECURITY
		// Gmail uses OAuth2, so no password needed
	case enum.EmailMailstack, enum.EmailGeneric:
		syncFolders = []string{models.MAILBOX_INBOX, models.MAILBOX_SENT, models.MAILBOX_SPAM}
		imapPort = models.MAILBOX_IMAP_PORT
		smtpPort = models.MAILBOX_SMTP_PORT
		imapServer = models.MAILBOX_IMAP_SERVER
		smtpServer = models.MAILBOX_SMTP_SERVER
		imapSecurity = models.MAILBOX_IMAP_SECURITY
		smtpSecurity = models.MAILBOX_SMTP_SECURITY
	default:
		err := fmt.Errorf("unsupported provider: %s", provider)
		spans.TraceError(err)
		return nil, err
	}

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

		ForwardingTo:    strings.Join(request.ForwardingTo, ","),
		WebmailEnabled:  request.WebmailEnabled,
		ProvisionStatus: models.MailboxStatusPendingProvisioning,
		RampUpRate:      3,
		RampUpMax:       40,
		RampUpCurrent:   3,
	}

	// Set provider-specific fields
	switch provider {
	case enum.EmailGoogleWorkspace:
		if oauth == nil {
			err := errors.New("OAuth data is required for Gmail")
			spans.TraceError(err)
			return nil, err
		}
		mailbox.ImapServer = imapServer
		mailbox.ImapPort = imapPort
		mailbox.ImapUsername = strings.ToLower(emailAddress)
		mailbox.ImapSecurity = imapSecurity

		mailbox.OAuthAccessToken = oauth.OAuthAccessToken
		mailbox.OAuthRefreshToken = oauth.OAuthRefreshToken
		mailbox.OAuthTokenExpiry = oauth.OAuthTokenExpiry
		mailbox.OAuthScope = oauth.OAuthScope
		mailbox.OAuthTokenId = oauth.OAuthTokenId
		// Mark as provisioned since Gmail doesn't need provisioning
		mailbox.ProvisionStatus = models.MailboxStatusProvisioned

	case enum.EmailMailstack, enum.EmailGeneric:
		mailbox.ImapServer = imapServer
		mailbox.ImapPort = imapPort
		mailbox.ImapUsername = strings.ToLower(emailAddress)
		mailbox.ImapPassword = request.Password
		mailbox.ImapSecurity = imapSecurity

		mailbox.SmtpServer = smtpServer
		mailbox.SmtpPort = smtpPort
		mailbox.SmtpUsername = strings.ToLower(emailAddress)
		mailbox.SmtpPassword = request.Password
		mailbox.SmtpSecurity = smtpSecurity
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
	spans, ctx := telemetry.StartServiceSpan(ctx, "MailboxService.RampUpMailboxes")
	defer spans.Finish()

	mailboxes, err := s.repositories.MailboxRepository.GetForRampUp(ctx)
	if err != nil {
		spans.TraceError(err)
		return err
	}

	spans.LogKV("mailboxes.count", len(mailboxes))

	for _, mailbox := range mailboxes {
		innerCtx := utils.WithTenantContext(ctx, mailbox.Tenant)
		err := s.rampUpMailbox(innerCtx, mailbox)
		if err != nil {
			spans.TraceError(err)
			// Continue processing other mailboxes even if one fails
			continue
		}
	}

	return nil
}

func (s *mailboxService) rampUpMailbox(ctx context.Context, mailbox *models.Mailbox) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "MailboxService.rampUpMailbox")
	defer spans.Finish()

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
			spans.TraceError(err)
			return err
		}
	}

	return nil
}

func (s *mailboxService) ConfigureMailbox(ctx context.Context, mailboxID string) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "MailboxService.ConfigureMailbox")
	defer spans.Finish()
	spans.TagEntity(mailboxID)

	tenant := utils.GetTenantFromContext(ctx)

	// Get the mailbox from the repository
	mailbox, err := s.repositories.MailboxRepository.GetMailbox(ctx, mailboxID)
	if err != nil {
		spans.TraceError(errors.Wrap(err, "failed to get mailbox"))
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
		spans.TraceError(errors.Wrap(err, "failed to configure mailbox with OpenSRS"))

		dbErr := s.repositories.MailboxRepository.ConfigureAttempt(ctx, mailbox.ID)
		if dbErr != nil {
			spans.TraceError(dbErr)
			return dbErr
		}

		return errors.Wrap(err, "failed to configure mailbox with OpenSRS")
	}

	// Update mailbox status to provisioned
	err = s.repositories.MailboxRepository.UpdateProvisionStatus(ctx, mailboxID, models.MailboxStatusProvisioned)
	if err != nil {
		spans.TraceError(errors.Wrap(err, "failed to update mailbox status"))
		return errors.Wrap(err, "failed to update mailbox status")
	}

	return nil
}

func (s *mailboxService) CreateMailbox(ctx context.Context, request interfaces.CreateMailboxRequest) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "MailboxService.CreateMailbox")
	defer spans.Finish()
	spans.LogObjectAsJson("request", request)

	if !request.IgnoreDomainOwnership {
		if err := s.validateCreateMailboxRequest(ctx, spans, request.Domain); err != nil {
			spans.TraceError(errors.Wrap(err, "cannot validate MailboxRequest"))
			return err
		}
	}

	mailboxEmailAddress := strings.ToLower(request.Username + "@" + request.Domain)

	// Verify mailbox doesn't exist
	if err := s.verifyMailboxNotExists(ctx, spans, mailboxEmailAddress); err != nil {
		spans.TraceError(errors.Wrap(err, "failed to verify mailbox does not exist"))
		return err
	}

	// Prepare mailbox using common method
	mailbox, err := s.prepareMailboxForSave(ctx, enum.EmailMailstack, mailboxEmailAddress, request, nil)
	if err != nil {
		spans.TraceError(errors.Wrap(err, "Error preparing mailbox"))
		return err
	}

	// Save mailbox
	mailboxID, err := s.repositories.MailboxRepository.SaveMailbox(ctx, *mailbox)
	if err != nil {
		spans.TraceError(errors.Wrap(err, "Error saving mailbox"))
		return err
	}
	if mailboxID == "" {
		return errors.New("failed to save mailbox")
	}
	spans.TagEntity(mailboxID)
	return nil
}

func (s *mailboxService) validateCreateMailboxRequest(ctx context.Context, spans *telemetry.Spans, domain string) error {
	if err := utils.ValidateTenant(ctx); err != nil {
		spans.TraceError(err)
		return err
	}

	tenant := utils.GetTenantFromContext(ctx)
	if domain != TEST_MAILBOX_DOMAIN {
		domainBelongsToTenant, err := s.repositories.DomainRepository.CheckDomainOwnership(ctx, tenant, domain)
		if err != nil {
			spans.TraceError(errors.Wrap(err, "Error checking domain"))
			return errors.Wrap(err, "Error checking domain")
		}
		if !domainBelongsToTenant {
			spans.TraceError(errors.Wrap(internalerrors.ErrDomainNotFound, "domain does not belong to tenant"))
			return internalerrors.ErrDomainNotFound
		}
	}
	return nil
}

func (s *mailboxService) verifyMailboxNotExists(ctx context.Context, spans *telemetry.Spans, mailboxEmail string) error {
	mboxCheck, err := s.repositories.MailboxRepository.GetMailboxByEmailAddressCrossTenant(ctx, mailboxEmail)
	if err != nil {
		spans.TraceError(err)
		return err
	}
	if mboxCheck != nil {
		spans.TraceError(internalerrors.ErrMailboxExists)
		return internalerrors.ErrMailboxExists
	}
	return nil
}

// GetMailboxes returns all mailboxes for a given domain
// If domain is empty, it returns all mailboxes for the tenant
func (s *mailboxService) GetMailboxes(ctx context.Context, provider enum.EmailProvider, domain, userId string) ([]*models.Mailbox, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "MailboxService.GetMailboxes")
	defer spans.Finish()
	spans.LogKV("domain", domain, "userId", userId, "provider", provider)

	// Get mailboxes with filters
	mailboxRecords, err := s.repositories.MailboxRepository.GetAllWithFilters(ctx, provider, domain, userId)
	if err != nil {
		spans.TraceError(errors.Wrap(err, "Error retrieving mailboxes"))
		return nil, err
	}
	spans.LogKV("result.count", len(mailboxRecords))
	return mailboxRecords, nil
}

func (s *mailboxService) GetMailboxByEmailAddress(ctx context.Context, emailAddress string) (*models.Mailbox, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "MailboxService.GetMailboxByEmailAddress")
	defer spans.Finish()
	spans.LogKV("emailAddress", emailAddress)

	mailboxRecord, err := s.repositories.MailboxRepository.GetMailboxByEmailAddress(ctx, emailAddress)
	if err != nil {
		spans.TraceError(errors.Wrap(err, "Error retrieving mailbox"))
		return nil, err
	}
	return mailboxRecord, nil
}
