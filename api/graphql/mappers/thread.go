package mappers

import (
	"github.com/customeros/mailstack/api/graphql/graphql_model"
	"github.com/customeros/mailstack/internal/models"
)

func MapGormThreadToGraph(thread *models.EmailThread) *graphql_model.EmailThread {
	return &graphql_model.EmailThread{
		ID:               thread.ID,
		UserID:           "user123",
		MailboxID:        thread.MailboxID,
		Subject:          thread.Subject,
		Summary:          "This is summary",
		IsViewed:         true,
		IsDone:           false,
		LastSender:       "Mihai",
		LastSenderDomain: "CustomerOS",
		LastMessageAt:    thread.LastMessageAt,
	}
}
