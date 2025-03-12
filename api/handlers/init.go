package handlers

import "github.com/customeros/mailstack/internal/repository"

type APIHandlers struct {
	Emails *EmailsHandler
}

func InitHandlers(r *repository.Repositories) *APIHandlers {
	return &APIHandlers{
		Emails: NewEmailsHandler(r),
	}
}
