package email

import (
	"context"

	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"

	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"
)

func (s *emailService) ScheduleSend(ctx context.Context, email *models.Email, attachmentIDs []string) (string, enum.EmailStatus, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailService.Send")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	err := s.validateEmail(ctx, email, attachmentIDs)
	if err != nil {
		tracing.TraceErr(span, err)
		return "", enum.EmailStatusFailed, err
	}

	// get sender profile if exists and needed to complete send

	// conplete emailRecord

	// create a new email thraed & attach email to it

	// queue email for sending & return -- handle immedeate and future sends
	return "", enum.EmailStatusQueued, nil

	// async process email send
}

func (s *emailService) validateEmail(ctx context.Context, email *models.Email, attachmentIDs []string) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailService.validateEmail")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	err := s.validateSender(ctx, email)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	// validate recipients are valid emails
	err = validateRecipients(email)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	// validate body and subject
	if email.Subject == "" {
		err = ErrEmptySubject
		tracing.TraceErr(span, err)
		return err
	}
	if email.BodyHTML == "" && email.BodyText == "" {
		err = ErrEmptyEmailBody
		tracing.TraceErr(span, err)
		return err
	}

	// validate attachments
	if attachmentIDs != nil {
		email.HasAttachment = true
		for _, attachment := range attachmentIDs {
			err := s.validateAttachment(ctx, attachment)
			if err != nil {
				tracing.TraceErr(span, err)
				return errors.Wrap(err, attachment)
			}
		}
	}

	// validate scheduledFor
	if email.ScheduledFor != nil && !utils.IsInFuture(*email.ScheduledFor) {
		err = ErrScheduledSendNotValid
		tracing.TraceErr(span, err)
		return err
	}
	return nil
}

func (s *emailService) validateAttachment(ctx context.Context, attachmentID string) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailService.validateAttachment")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	attachment, err := s.repositories.EmailAttachmentRepository.GetByID(ctx, attachmentID)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}
	if attachment == nil {
		err = ErrAttachmentDoesNotExist
		tracing.TraceErr(span, err)
		return err
	}
	return nil
}

func validateRecipients(email *models.Email) error {
	if len(email.ToAddresses) == 0 {
		err := ErrRecipientsMissing
		return err
	}

	for i := range email.ToAddresses {
		err := ValidateEmailAddress(&email.ToAddresses[i])
		if err != nil {
			return errors.Wrap(err, email.ToAddresses[i])
		}
	}

	for i := range email.CcAddresses {
		err := ValidateEmailAddress(&email.CcAddresses[i])
		if err != nil {
			return errors.Wrap(err, email.CcAddresses[i])
		}
	}

	for i := range email.BccAddresses {
		err := ValidateEmailAddress(&email.BccAddresses[i])
		if err != nil {
			return errors.Wrap(err, email.BccAddresses[i])
		}
	}
	return nil
}

func (s *emailService) validateSender(ctx context.Context, email *models.Email) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailService.validateSender")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	// validate mailbox/email exists
	var mailbox *models.Mailbox
	var err error

	if email.MailboxID != "" {
		mailbox, err = s.repositories.MailboxRepository.GetMailbox(ctx, email.MailboxID)
	} else {
		if email.FromAddress == "" {
			err = ErrUnknownSender
			tracing.TraceErr(span, err)
			return err
		}
		mailbox, err = s.repositories.MailboxRepository.GetMailboxByEmailAddress(ctx, email.FromAddress)
	}
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}
	if mailbox == nil {
		err = ErrMailboxDoesNotExist
		tracing.TraceErr(span, err)
		return err
	}

	// validate user/tenant owns mailbox/email
	tenant := utils.GetTenantFromContext(ctx)
	if mailbox.Tenant != tenant {
		tracing.TraceErr(span, ErrUnauthorizedSender)
		span.LogKV("mailboxTenant", mailbox.Tenant)
		span.LogKV("ctxTenant", tenant)
		return ErrUnauthorizedSender
	}
	userID := utils.GetUserIdFromContext(ctx)
	if mailbox.UserID != userID {
		tracing.TraceErr(span, ErrUnauthorizedSender)
		span.LogKV("mailboxUserId", mailbox.UserID)
		span.LogKV("ctxTenant", userID)
		return ErrUnauthorizedSender
	}

	// validate outbound enabled
	if !mailbox.OutboundEnabled {
		err = ErrOutboundNotEnabled
		tracing.TraceErr(span, err)
		return err
	}

	// validate sender profile exists or sender info provided in request
	if mailbox.SenderID == "" && email.FromName == "" {
		err = ErrUnknownSender
		tracing.TraceErr(span, err)
		span.LogKV("noSenderProfile", true)
		return err
	}

	if mailbox.ReplyToAddress != "" && email.ReplyTo == "" {
		email.ReplyTo = mailbox.ReplyToAddress
	}
	return nil
}
