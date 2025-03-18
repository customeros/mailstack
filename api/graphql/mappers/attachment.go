package mappers

import (
	"github.com/customeros/mailstack/api/graphql/graphql_model"
	"github.com/customeros/mailstack/internal/models"
)

func MapGormAttachmentToGraph(attachment *models.EmailAttachment) *graphql_model.Attachment {
	return &graphql_model.Attachment{
		ID:          attachment.ID,
		EmailID:     attachment.EmailID,
		Filename:    attachment.Filename,
		ContentType: attachment.ContentType,
		// TODO implement attachment URLs behind CDN
		URL: "https://attachment.dummyurl.com",
	}
}
