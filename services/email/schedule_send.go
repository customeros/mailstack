package email

import (
	"context"

	"github.com/customeros/mailsherpa/mailvalidate"
	"github.com/pkg/errors"

	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/telemetry"
	"github.com/customeros/mailstack/internal/utils"
)

func (s *emailService) ScheduleSend(ctx context.Context, email *models.EmailLog, attachmentIDs []string) (string, enum.EmailStatus, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailService.ScheduleSend")
	defer spans.Finish()

	err := s.validateEmail(ctx, email, attachmentIDs)
	if err != nil {
		spans.TraceError(err)
		return "", enum.EmailStatusFailed, err
	}

	setDefaultSendingValues(email)

	// create a new email thraed & attach email to it
	err = s.createNewEmailThreadForEmail(ctx, email)
	if err != nil {
		spans.TraceError(err)
		return "", enum.EmailStatusFailed, err
	}

	// save email to db
	if email.ScheduledFor != nil {
		email.Status = enum.EmailStatusScheduled
	}
	// TODO FIX THIS
	emailID, err := s.repositories.EmailRepository.Create(ctx, &models.Email{})
	if err != nil {
		spans.TraceError(err)
		return "", enum.EmailStatusFailed, err
	}

	// TODO if scheduleFor is empty, fire event to send now

	return emailID, enum.EmailStatus(email.Status), nil
}

func (s *emailService) createNewEmailThreadForEmail(ctx context.Context, email *models.EmailLog) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailService.createNewEmailThreadForEmail")
	defer spans.Finish()

	thread := &models.EmailThread{
		MailboxID:      email.MailboxID,
		Subject:        email.Subject,
		Participants:   []string{}, // TODO FIX THIS
		LastMessageID:  email.MessageID,
		HasAttachments: email.HasAttachment,
		FirstMessageAt: utils.NowPtr(),
		LastMessageAt:  utils.NowPtr(),
	}

	threadID, err := s.repositories.EmailThreadRepository.Create(ctx, thread)
	if err != nil {
		spans.TraceError(err)
		return err
	}
	if threadID == "" {
		err = errors.New("failed to create new email thread")
		spans.TraceError(err)
		return err
	}

	email.ThreadID = threadID
	return nil
}

func setDefaultSendingValues(email *models.EmailLog) {
	email.Direction = enum.EmailDirectionOutbound
	email.Status = enum.EmailStatusQueued
	email.MessageID = utils.GenerateMessageID(email.FromDomain, "")
}

func (s *emailService) validateEmail(ctx context.Context, email *models.EmailLog, attachmentIDs []string) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailService.validateEmail")
	defer spans.Finish()

	err := s.validateSender(ctx, email)
	if err != nil {
		spans.TraceError(err)
		return err
	}

	// validate recipients are valid emails
	err = validateRecipients(email)
	if err != nil {
		spans.TraceError(err)
		return err
	}

	// validate body and subject
	if email.Subject == "" {
		err = ErrEmptySubject
		spans.TraceError(err)
		return err
	}
	if email.BodyHtml == "" && email.BodyText == "" {
		err = ErrEmptyEmailBody
		spans.TraceError(err)
		return err
	}

	// validate attachments
	if attachmentIDs != nil {
		email.HasAttachment = true
		for _, attachment := range attachmentIDs {
			err := s.validateAttachment(ctx, attachment)
			if err != nil {
				spans.TraceError(err)
				return errors.Wrap(err, attachment)
			}
		}
	}

	// validate scheduledFor
	if email.ScheduledFor != nil && !utils.IsInFuture(*email.ScheduledFor) {
		err = ErrScheduledSendNotValid
		spans.TraceError(err)
		return err
	}
	return nil
}

func (s *emailService) validateAttachment(ctx context.Context, attachmentID string) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailService.validateAttachment")
	defer spans.Finish()

	attachment, err := s.repositories.EmailAttachmentRepository.GetByID(ctx, attachmentID)
	if err != nil {
		spans.TraceError(err)
		return err
	}
	if attachment == nil {
		err = ErrAttachmentDoesNotExist
		spans.TraceError(err)
		return err
	}
	return nil
}

func validateRecipients(email *models.EmailLog) error {
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

func (s *emailService) validateSender(ctx context.Context, email *models.EmailLog) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailService.validateSender")
	defer spans.Finish()

	// validate mailbox/email exists
	mailbox, err := s.getMailbox(ctx, email.MailboxID, email.FromAddress)
	if err != nil {
		spans.TraceError(err)
		return err
	}
	if mailbox == nil {
		err = ErrMailboxDoesNotExist
		spans.TraceError(err)
		return err
	}
	email.MailboxID = mailbox.ID

	// validate user/tenant owns mailbox/email
	tenant := utils.GetTenantFromContext(ctx)
	if mailbox.Tenant != tenant {
		spans.TraceError(ErrUnauthorizedSender)
		spans.LogKV("mailboxTenant", mailbox.Tenant)
		spans.LogKV("ctxTenant", tenant)
		return ErrUnauthorizedSender
	}
	userID := utils.GetUserIdFromContext(ctx)
	if mailbox.UserID != userID {
		spans.TraceError(ErrUnauthorizedSender)
		spans.LogKV("mailboxUserId", mailbox.UserID)
		spans.LogKV("ctxTenant", userID)
		return ErrUnauthorizedSender
	}

	// validate outbound enabled
	if !mailbox.OutboundEnabled {
		err = ErrOutboundNotEnabled
		spans.TraceError(err)
		return err
	}

	// validate sender email and set user and domain on email
	validateSender := mailvalidate.ValidateEmailSyntax(email.FromAddress)
	if !validateSender.IsValid || validateSender.IsSystemGenerated || validateSender.IsFreeAccount {
		err = ErrInvalidSender
		spans.TraceError(err)
		return err
	}
	email.FromUser = validateSender.User
	email.FromDomain = validateSender.Domain

	// validate sender profile exists or sender info provided in request
	if mailbox.SenderID == "" && email.FromName == "" {
		err = ErrUnknownSender
		spans.TraceError(err)
		spans.LogKV("noSenderProfile", true)
		return err
	}

	err = s.buildEmailSender(ctx, email, mailbox)
	if err != nil {
		spans.TraceError(err)
		return err
	}

	if mailbox.ReplyToAddress != "" && email.ReplyTo == "" {
		email.ReplyTo = mailbox.ReplyToAddress
	}

	return nil
}

func (s *emailService) getMailbox(ctx context.Context, mailboxID, fromAddress string) (*models.Mailbox, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailService.getMailbox")
	defer spans.Finish()

	if mailboxID != "" {
		return s.repositories.MailboxRepository.GetMailbox(ctx, mailboxID)
	}

	if fromAddress == "" {
		err := ErrUnknownSender
		spans.TraceError(err)
		return nil, err
	}

	return s.repositories.MailboxRepository.GetMailboxByEmailAddress(ctx, fromAddress)
}

// buildEmailSender fills in sender details.  Values provided in the email request override default values
// attached to senderID.  SenderID only used to fill in gaps in the request.
func (s *emailService) buildEmailSender(ctx context.Context, email *models.EmailLog, mailbox *models.Mailbox) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "emailService.buildEmailSender")
	defer spans.Finish()

	if mailbox.SenderID == "" {
		return nil
	}

	if email.FromName != "" {
		return nil
	}

	// get sender
	sender, err := s.repositories.SenderRepository.GetByID(ctx, mailbox.SenderID)
	if err != nil {
		spans.TraceError(err)
		return err
	}
	if sender == nil {
		spans.TraceError(ErrUnknownSender)
		return ErrUnknownSender
	}

	email.FromName = sender.DisplayName

	// TODO add signatures to outgoing emails
	return nil
}
