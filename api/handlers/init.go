package handlers

import "github.com/customeros/mailstack/internal/repository"

type APIHandlers struct {
	Emails  *EmailsHandler
	Domains *DomainHandler
}

func InitHandlers(r *repository.Repositories) *APIHandlers {
	return &APIHandlers{
		Emails:  NewEmailsHandler(r),
		Domains: NewDomainHandler(r),
	}
}
