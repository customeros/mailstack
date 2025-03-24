package dbmapper

import (
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
)

func MapEmailStoreToEmail(rawEmail *models.EmailStore) *models.Email {
	return &models.Email{
		ID:            rawEmail.ID,
		MailboxID:     rawEmail.MailboxID,
		Direction:     enum.EmailDirection(rawEmail.Direction),
		Status:        enum.EmailStatus(rawEmail.Status),
		MessageID:     rawEmail.MessageID,
		ThreadID:      rawEmail.ThreadID,
		Subject:       rawEmail.Subject,
		FromAddress:   rawEmail.FromAddress,
		FromName:      rawEmail.FromName,
		FromUser:      rawEmail.FromUser,
		FromDomain:    rawEmail.FromDomain,
		ReplyTo:       rawEmail.ReplyTo,
		ToAddresses:   rawEmail.ToAddresses,
		CcAddresses:   rawEmail.CcAddresses,
		BccAddresses:  rawEmail.BccAddresses,
		TrackClicks:   rawEmail.TrackClicks,
		Body:          rawEmail.BodyMarkdown,
		HasAttachment: rawEmail.HasAttachment,
		SentAt:        rawEmail.SentAt,
		ReceivedAt:    rawEmail.ReceivedAt,
		LastAttemptAt: rawEmail.LastAttemptAt,
		ScheduledFor:  rawEmail.ScheduledFor,
	}
}
