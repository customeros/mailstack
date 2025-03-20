package email

import (
	"errors"

	"github.com/customeros/mailsherpa/mailvalidate"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/services/events"
)

type emailService struct {
	eventsService *events.EventsService
	repositories  *repository.Repositories
}

func NewEmailService(
	eventsService *events.EventsService,
	repositories *repository.Repositories,
) interfaces.EmailService {
	return &emailService{
		repositories:  repositories,
		eventsService: eventsService,
	}
}

var (
	ErrMailboxDoesNotExist    = errors.New("mailbox does not exist")
	ErrUnknownSender          = errors.New("unknown sender")
	ErrUnauthorizedSender     = errors.New("unauthorized sender")
	ErrOutboundNotEnabled     = errors.New("sending not enabled on this mailbox")
	ErrRecipientsMissing      = errors.New("recipients missing")
	ErrInvalidEmail           = errors.New("email address is invalid")
	ErrEmptySubject           = errors.New("empty subject")
	ErrEmptyEmailBody         = errors.New("empty email body")
	ErrAttachmentDoesNotExist = errors.New("attachment does not exist")
	ErrScheduledSendNotValid  = errors.New("invalid scheduled for time")
	ErrInvalidSender          = errors.New("invalid sender")
)

func ValidateEmailAddress(email *string) error {
	validate := mailvalidate.ValidateEmailSyntax(*email)
	if !validate.IsValid || validate.IsSystemGenerated {
		return ErrInvalidEmail
	}
	email = &validate.CleanEmail
	return nil
}
