package email

import (
	"context"

	"github.com/customeros/mailsherpa/mailvalidate"
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

	setDefaultSendingValues(email)

	// create a new email thraed & attach email to it
	err = s.createNewEmailThreadForEmail(ctx, email)
	if err != nil {
		tracing.TraceErr(span, err)
		return "", enum.EmailStatusFailed, err
	}

	// save email to db
	emailID, err := s.repositories.EmailRepository.Create(ctx, email)
	if err != nil {
		tracing.TraceErr(span, err)
		return "", enum.EmailStatusFailed, err
	}

	// if scheduleFor is empty, fire event to send now
	return emailID, email.Status, nil
}

func (s *emailService) createNewEmailThreadForEmail(ctx context.Context, email *models.Email) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailService.createNewEmailThreadForEmail")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	thread := &models.EmailThread{
		MailboxID:      email.MailboxID,
		Subject:        email.Subject,
		Participants:   email.AllParticipants(),
		LastMessageID:  email.MessageID,
		HasAttachments: email.HasAttachment,
		FirstMessageAt: utils.NowPtr(),
		LastMessageAt:  utils.NowPtr(),
	}

	threadID, err := s.repositories.EmailThreadRepository.Create(ctx, thread)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}
	if threadID == "" {
		err = errors.New("failed to create new email thread")
		tracing.TraceErr(span, err)
		return err
	}

	email.ThreadID = threadID
	return nil
}

func setDefaultSendingValues(email *models.Email) {
	email.Direction = enum.EmailDirectionOutbound
	email.Status = enum.EmailStatusQueued
	email.MessageID = utils.GenerateMessageID(email.FromDomain, "")
	email.SendAttempts = 1
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
	mailbox, err := s.getMailbox(ctx, email)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}
	if mailbox == nil {
		err = ErrMailboxDoesNotExist
		tracing.TraceErr(span, err)
		return err
	}
	email.MailboxID = mailbox.ID

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

	// validate sender email and set user and domain on email
	validateSender := mailvalidate.ValidateEmailSyntax(email.FromAddress)
	if !validateSender.IsValid || validateSender.IsSystemGenerated || validateSender.IsFreeAccount {
		err = ErrInvalidSender
		tracing.TraceErr(span, err)
		return err
	}
	email.FromUser = validateSender.User
	email.FromDomain = validateSender.Domain

	// validate sender profile exists or sender info provided in request
	if mailbox.SenderID == "" && email.FromName == "" {
		err = ErrUnknownSender
		tracing.TraceErr(span, err)
		span.LogKV("noSenderProfile", true)
		return err
	}

	err = s.buildEmailSender(ctx, email, mailbox)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	if mailbox.ReplyToAddress != "" && email.ReplyTo == "" {
		email.ReplyTo = mailbox.ReplyToAddress
	}

	return nil
}

func (s *emailService) getMailbox(ctx context.Context, email *models.Email) (*models.Mailbox, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailService.getMailbox")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	if email.MailboxID != "" {
		return s.repositories.MailboxRepository.GetMailbox(ctx, email.MailboxID)
	}

	if email.FromAddress == "" {
		err := ErrUnknownSender
		tracing.TraceErr(span, err)
		return nil, err
	}

	return s.repositories.MailboxRepository.GetMailboxByEmailAddress(ctx, email.FromAddress)
}

// buildEmailSender fills in sender details.  Values provided in the email request override default values
// attached to senderID.  SenderID only used to fill in gaps in the request.
func (s *emailService) buildEmailSender(ctx context.Context, email *models.Email, mailbox *models.Mailbox) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "emailService.buildEmailService")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	if mailbox.SenderID == "" {
		return nil
	}

	if email.FromName != "" {
		return nil
	}

	// get sender
	sender, err := s.repositories.SenderRepository.GetByID(ctx, mailbox.SenderID)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}
	if sender == nil {
		tracing.TraceErr(span, ErrUnknownSender)
		return ErrUnknownSender
	}

	email.FromName = sender.DisplayName

	// TODO add signatures to outgoing emails
	return nil
}
