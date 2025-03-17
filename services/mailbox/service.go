package mailbox

import (
	"context"
	"strings"

	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
	"github.com/pkg/errors"
	"gorm.io/gorm"

	"github.com/customeros/mailstack/interfaces"
	er "github.com/customeros/mailstack/internal/errors"
	"github.com/customeros/mailstack/internal/logger"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"
)

type mailboxService struct {
	log      logger.Logger
	postgres *repository.Repositories
}

const TEST_MAILBOX_DOMAIN = "testcustomeros.com"

func NewMailboxService(log logger.Logger, postgres *repository.Repositories) interfaces.MailboxService {
	return &mailboxService{
		log:      log,
		postgres: postgres,
	}
}

func (s *mailboxService) CreateMailbox(ctx context.Context, tx *gorm.DB, request interfaces.CreateMailboxRequest) error {
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
	defer span.Finish()

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
	if err := s.createMailbox(ctx, span, tx, request, mailboxEmail, request.UserId); err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "failed to save mailbox settings"))
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
		domainBelongsToTenant, err := s.postgres.DomainRepository.CheckDomainOwnership(ctx, tenant, domain)
		if err != nil {
			tracing.TraceErr(span, errors.Wrap(err, "Error checking domain"))
			return errors.Wrap(err, "Error checking domain")
		}
		if !domainBelongsToTenant {
			tracing.TraceErr(span, errors.Wrap(er.ErrDomainNotFound, "domain does not belong to tenant"))
			return er.ErrDomainNotFound
		}
	}
	return nil
}

func (s *mailboxService) verifyMailboxNotExists(ctx context.Context, span opentracing.Span, mailboxEmail string) error {
	mailboxRecord, err := s.postgres.TenantSettingsMailboxRepository.GetByMailbox(ctx, mailboxEmail)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "Error checking mailbox"))
		return err
	}
	if mailboxRecord != nil {
		tracing.TraceErr(span, errors.Wrap(er.ErrMailboxExists, "Mailbox exists"))
		return er.ErrMailboxExists
	}
	return nil
}

func (s *mailboxService) createMailbox(ctx context.Context, span opentracing.Span, tx *gorm.DB, request interfaces.CreateMailboxRequest, mailboxEmail string, userId string) error {
	tenant := utils.GetTenantFromContext(ctx)
	tenantSettingsMailbox := models.TenantSettingsMailbox{
		Tenant:          tenant,
		Domain:          request.Domain,
		MailboxUsername: mailboxEmail,
		MailboxPassword: request.Password,
		Username:        request.Username,
		UserId:          userId,
		ForwardingTo:    strings.Join(request.ForwardingTo, ","),
		WebmailEnabled:  request.WebmailEnabled,
		Status:          models.MailboxStatusPendingProvisioning,
	}
	err := s.postgres.TenantSettingsMailboxRepository.Merge(ctx, tx, &tenantSettingsMailbox)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "Error saving mailbox"))
		return err
	}
	return nil
}

// GetMailboxes returns all mailboxes for a given domain
// If domain is empty, it returns all mailboxes for the tenant
func (s *mailboxService) GetMailboxes(ctx context.Context, domain string) ([]*models.TenantSettingsMailbox, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "MailboxService.GetMailboxes")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)
	span.LogFields(
		log.String("domain", domain),
	)

	// if domain is missing return all mailboxes for tenant
	if domain == "" {
		mailboxRecords, err := s.postgres.TenantSettingsMailboxRepository.GetAll(ctx)
		if err != nil {
			tracing.TraceErr(span, errors.Wrap(err, "Error retrieving mailboxes"))
			return nil, err
		}
		return mailboxRecords, nil
	}
	mailboxRecords, err := s.postgres.TenantSettingsMailboxRepository.GetAllByDomain(ctx, domain)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "Error retrieving mailboxes"))
		return nil, err
	}
	return mailboxRecords, nil
}

func (s *mailboxService) GetByMailbox(ctx context.Context, username, domain string) (*models.TenantSettingsMailbox, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "MailboxService.GetByMailbox")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)
	span.LogFields(
		log.String("username", username),
		log.String("domain", domain),
	)
	mailboxRecord, err := s.postgres.TenantSettingsMailboxRepository.GetByMailbox(ctx, username+"@"+domain)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "Error retrieving mailbox"))
		return nil, err
	}
	return mailboxRecord, nil
}

func (s *mailboxService) RampUpMailboxes(ctx context.Context) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "MailboxService.RampUpMailboxes")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	mailboxes, err := s.postgres.TenantSettingsMailboxRepository.GetForRampUp(ctx)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	span.LogFields(log.Int("mailboxes.count", len(mailboxes)))

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

func (s *mailboxService) rampUpMailbox(ctx context.Context, mailbox *models.TenantSettingsMailbox) error {
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

		err := s.postgres.TenantSettingsMailboxRepository.UpdateRampUpFields(ctx, mailbox)
		if err != nil {
			tracing.TraceErr(span, err)
			return err
		}
	}

	return nil
}
