package emails

// import (
// 	"context"
// 	"fmt"
// 	"strings"
//
// 	"github.com/customeros/mailsherpa/mailvalidate"
// 	"github.com/opentracing/opentracing-go"
// 	"github.com/pkg/errors"
//
// 	"github.com/customeros/mailstack/internal/models"
// 	"github.com/customeros/mailstack/internal/tracing"
// 	"github.com/customeros/mailstack/services/smtp"
// )
//
// // validateEmailSyntax validates and cleans a single email address
// func (h *EmailsHandler) validateEmailSyntax(ctx context.Context, emailAddress string) (string, error) {
// 	span, ctx := opentracing.StartSpanFromContext(ctx, "EmailsHandler.validateEmailSyntax")
// 	defer span.Finish()
//
// 	if emailAddress == "" {
// 		return "", errors.New("email address is empty")
// 	}
//
// 	validation := mailvalidate.ValidateEmailSyntax(emailAddress)
// 	if !validation.IsValid {
// 		return "", errors.New("invalid email format")
// 	}
//
// 	return validation.CleanEmail, nil
// }
//
// // resolveAttachments resolves attachment IDs to actual attachments
// func (h *EmailsHandler) resolveAttachments(ctx context.Context, attachmentRefs []Attachments) ([]*models.EmailAttachment, error) {
// 	span, ctx := opentracing.StartSpanFromContext(ctx, "EmailsHandler.resolveAttachments")
// 	defer span.Finish()
//
// 	if len(attachmentRefs) == 0 {
// 		return []*models.EmailAttachment{}, nil
// 	}
//
// 	validAttachments := make([]*models.EmailAttachment, 0, len(attachmentRefs))
// 	var invalidIDs []string
//
// 	for _, ref := range attachmentRefs {
// 		attachment, err := h.repositories.EmailAttachmentRepository.GetByID(ctx, ref.ID)
// 		if err != nil {
// 			tracing.TraceErr(span, err)
// 			invalidIDs = append(invalidIDs, ref.ID)
// 			continue
// 		}
// 		validAttachments = append(validAttachments, attachment)
// 	}
//
// 	if len(invalidIDs) > 0 {
// 		return nil, fmt.Errorf("invalid attachment IDs: %s", strings.Join(invalidIDs, ", "))
// 	}
//
// 	return validAttachments, nil
// }
//
// func (h *EmailsHandler) sendEmail(ctx context.Context, emailContainer *EmailContainer) error {
// 	span, ctx := opentracing.StartSpanFromContext(ctx, "EmailsHandler.sendEmail")
// 	defer span.Finish()
// 	tracing.TagComponentRest(span)
//
// 	// Save the email
// 	emailID, err := h.repositories.EmailRepository.Create(ctx, emailContainer.Email)
// 	if err != nil {
// 		return nil
// 	}
// 	emailContainer.Email.ID = emailID
//
// 	// Send the email
// 	smtpClient := smtp.NewSMTPClient(h.repositories, emailContainer.Mailbox)
// 	_ = smtpClient.Send(ctx, emailContainer.Email, emailContainer.Attachments)
//
// 	return nil
// }
