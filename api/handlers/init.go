package handlers

import (
	"github.com/customeros/mailstack/internal/config"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/services"
)

type APIHandlers struct {
	Emails  *EmailsHandler
	Domains *DomainHandler
	DNS     *DNSHandler
}

func InitHandlers(r *repository.Repositories, cfg *config.Config, s *services.Services) *APIHandlers {
	return &APIHandlers{
		Emails:  NewEmailsHandler(r),
		Domains: NewDomainHandler(r, cfg, s),
		DNS:     NewDNSHandler(s),
	}
}
