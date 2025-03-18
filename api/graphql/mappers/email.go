package mappers

import (
	"github.com/customeros/mailstack/api/graphql/graphql_model"
	"github.com/customeros/mailstack/internal/models"
)

func MapGormEmailToGraph(email *models.Email) *graphql_model.EmailMessage {
	return &graphql_model.EmailMessage{
		ID:         email.ID,
		ThreadID:   email.ThreadID,
		MailboxID:  email.MailboxID,
		Direction:  email.Direction,
		From:       email.FromAddress,
		To:         email.ToAddresses,
		Cc:         email.CcAddresses,
		Bcc:        email.BccAddresses,
		Subject:    email.CleanSubject,
		Body:       email.BodyMarkdown,
		ReceivedAt: *email.SentAt,
	}
}
