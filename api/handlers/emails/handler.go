package emails

import "github.com/customeros/mailstack/internal/repository"

type EmailsHandler struct {
	repositories *repository.Repositories
}

func NewEmailsHandler(repos *repository.Repositories) *EmailsHandler {
	return &EmailsHandler{
		repositories: repos,
	}
}
