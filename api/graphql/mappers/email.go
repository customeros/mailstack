package mappers

import (
	"github.com/customeros/mailstack/api/graphql/graphql_model"
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/utils"
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

func MapGraphEmailInputToGorm(email *graphql_model.EmailInput) *models.Email {
	return &models.Email{
		MailboxID:    utils.GetOrDefault(email.MailboxID, ""),
		Direction:    enum.EmailDirectionOutbound,
		FromAddress:  email.FromAddress,
		FromName:     utils.GetOrDefault(email.FromName, ""),
		ToAddresses:  email.ToAddresses,
		CcAddresses:  email.CcAddresses,
		BccAddresses: email.BccAddresses,
		ReplyTo:      utils.GetOrDefault(email.ReplyTo, ""),
		Subject:      email.Subject,
		BodyText:     utils.GetOrDefault(email.Body.Text, ""),
		BodyHTML:     utils.GetOrDefault(email.Body.HTML, ""),
		ScheduledFor: email.ScheduleFor,
		TrackClicks:  utils.GetOrDefault(email.TrackClicks, false),
	}
}
