package mappers

import (
	"github.com/customeros/mailstack/api/graphql/graphql_model"
	"github.com/customeros/mailstack/internal/models"
)

func MapGormMailboxToGraph(mailbox *models.Mailbox) *graphql_model.Mailbox {
	return &graphql_model.Mailbox{
		ID:                     mailbox.ID,
		Provider:               mailbox.Provider,
		EmailAddress:           mailbox.EmailAddress,
		SenderID:               &mailbox.SenderID,
		InboundEnabled:         mailbox.InboundEnabled,
		OutboundEnabled:        mailbox.OutboundEnabled,
		ReplyToAddress:         &mailbox.ReplyToAddress,
		ConnectionStatus:       mailbox.ConnectionStatus,
		LastConnectionCheck:    *mailbox.LastConnectionCheck,
		ConnectionErrorMessage: &mailbox.ErrorMessage,
	}
}
