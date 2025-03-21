package mappers

import (
	"github.com/customeros/mailstack/api/graphql/graphql_model"
	"github.com/customeros/mailstack/internal/models"
)

// TODO implement thread summary mappings
func MapGormThreadToGraph(thread *models.EmailThread) *graphql_model.EmailThread {
	return &graphql_model.EmailThread{
		ID:            thread.ID,
		MailboxID:     thread.MailboxID,
		Subject:       thread.Subject,
		Summary:       "This is summary",
		IsDone:        thread.IsDone,
		LastMessageAt: thread.LastMessageAt,
	}
}
