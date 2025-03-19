package interfaces

import (
	"context"

	"github.com/customeros/mailstack/api/graphql/graphql_model"
	"github.com/customeros/mailstack/internal/models"
)

type MailboxService interface {
	EnrollMailbox(ctx context.Context, mailbox *graphql_model.MailboxInput) (*models.Mailbox, error)
}
