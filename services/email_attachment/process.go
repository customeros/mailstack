package email_attachment

import (
	"context"
	"io"

	"github.com/nats-io/nats.go"
	"go.uber.org/multierr"

	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/telemetry"
	"github.com/customeros/mailstack/proto/pb"
)

func (s *EmailAttachmentService) processAttachments(ctx context.Context, request *pb.ProcessAttachmentRequest) *pb.ProcessAttachmentResponse {
	spans, ctx := telemetry.StartServiceSpan(ctx, "EmailAttachmentService.processAttachments")
	defer spans.Finish()
	spans.LogObjectAsJson("request", request)

	response := &pb.ProcessAttachmentResponse{
		EmailId: request.EmailId,
	}

	if len(request.Attachments) == 0 {
		return response
	}
	response.HasAttachment = true

	var errs error
	for _, attachment := range request.Attachments {
		fileID, err := s.processAttachment(ctx, attachment, request.EmailId, request.MailboxId)
		if err != nil {
			spans.TraceError(err)
			errs = multierr.Append(errs, err)
		}
		response.AttachmentIds = append(response.AttachmentIds, fileID)
	}
	if errs != nil {
		response.ErrorMessage = errs.Error()
	}

	return response
}

func (s *EmailAttachmentService) processAttachment(ctx context.Context, attachment *pb.AttachmentMetadata, emailID, mailboxID string) (string, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "EmailAttachmentService.processAttachment")
	defer spans.Finish()
	spans.TagEntity(emailID)
	spans.LogKV("mailboxID", mailboxID)

	if attachment == nil {
		return "", nil
	}

	bucket, err := s.natsConn.JS.ObjectStore(enum.NATSBucketEmailAttachment.String())
	if err != nil {
		spans.TraceError(err)
		return "", err
	}

	content, err := s.getFileFromNATSCache(ctx, bucket, attachment.StorageKey)
	if err != nil {
		spans.TraceError(err)
		return "", err
	}

	attachmentRecord := &models.EmailAttachment{
		Filename:    attachment.Filename,
		Size:        int(attachment.Size),
		ContentType: attachment.ContentType,
		ContentID:   attachment.ContentId,
		IsInline:    attachment.IsInline,
	}

	fileID, err := s.repositories.EmailAttachmentRepository.Store(ctx, attachmentRecord, emailID, mailboxID, content)
	if err != nil {
		spans.TraceError(err)
		return "", err
	}

	err = s.deleteFileFromNATSCache(ctx, bucket, attachment.StorageKey)
	if err != nil {
		spans.TraceError(err)
		return "", err
	}

	return fileID, nil
}

func (s *EmailAttachmentService) getFileFromNATSCache(ctx context.Context, bucket nats.ObjectStore, fileKey string) ([]byte, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "EmailAttachmentService.getFileFromNATSCache")
	defer spans.Finish()

	obj, err := bucket.Get(fileKey)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}

	// Read the file data
	data, err := io.ReadAll(obj)
	if err != nil {
		spans.TraceError(err)
		return nil, err
	}
	obj.Close()

	return data, nil
}

func (s *EmailAttachmentService) deleteFileFromNATSCache(ctx context.Context, bucket nats.ObjectStore, fileKey string) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "EmailAttachmentService.deleteFileFromNATSCache")
	defer spans.Finish()

	return bucket.Delete(fileKey)
}
