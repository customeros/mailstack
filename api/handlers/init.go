package handlers

import (
	"github.com/customeros/mailstack/api/handlers/emails"
	"github.com/customeros/mailstack/internal/config"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/services"
)

type APIHandlers struct {
	Emails   *emails.EmailsHandler
	Domains  *DomainHandler
	DNS      *DNSHandler
	Mailbox  *MailboxHandler
	Postmark *PostmarkHandler
}

func InitHandlers(r *repository.Repositories, cfg *config.Config, s *services.Services) *APIHandlers {
	return &APIHandlers{
		Emails:   emails.NewEmailsHandler(r),
		Domains:  NewDomainHandler(r, cfg, s),
		DNS:      NewDNSHandler(s),
		Mailbox:  NewMailboxHandler(r, cfg, s),
		Postmark: NewPostmarkHandler(r, s),
	}
}
