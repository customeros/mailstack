package pb_mappers

import (
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/proto/pb"
)

// MailstackServiceToServiceName converts a Go enum MailstackService to protobuf ServiceName
func MailstackServiceToServiceName(service enum.MailstackService) pb.ServiceName {
	switch service {
	case enum.MailstackIMAPService:
		return pb.ServiceName_MAILSTACK_IMAP_SERVICE
	case enum.MailstackStorageService:
		return pb.ServiceName_MAILSTACK_STORAGE_SERVICE
	case enum.MailstackClassificationService:
		return pb.ServiceName_MAILSTACK_CLASSIFICATION_SERVICE
	case enum.MailstackAnalysisService:
		return pb.ServiceName_MAILSTACK_ANALYSIS_SERVICE
	case enum.MailstackAttachmentsService:
		return pb.ServiceName_MAILSTACK_ATTACHMENT_SERVICE
	case enum.MailstackThreadingService:
		return pb.ServiceName_MAILSTACK_THREADING_SERVICE
	case enum.MailstackContentService:
		return pb.ServiceName_MAILSTACK_CONTENT_SERVICE
	case enum.MailstackEventLoggerService:
		return pb.ServiceName_MAILSTACK_EVENT_LOGGER_SERVICE
	default:
		return pb.ServiceName_SERVICE_UNKNOWN
	}
}

// ServiceNameToMailstackService converts a protobuf ServiceName to Go enum MailstackService
func ServiceNameToMailstackService(serviceName pb.ServiceName) enum.MailstackService {
	switch serviceName {
	case pb.ServiceName_MAILSTACK_IMAP_SERVICE:
		return enum.MailstackIMAPService
	case pb.ServiceName_MAILSTACK_STORAGE_SERVICE:
		return enum.MailstackStorageService
	case pb.ServiceName_MAILSTACK_CLASSIFICATION_SERVICE:
		return enum.MailstackClassificationService
	case pb.ServiceName_MAILSTACK_ANALYSIS_SERVICE:
		return enum.MailstackAnalysisService
	case pb.ServiceName_MAILSTACK_ATTACHMENT_SERVICE:
		return enum.MailstackAttachmentsService
	case pb.ServiceName_MAILSTACK_THREADING_SERVICE:
		return enum.MailstackThreadingService
	case pb.ServiceName_MAILSTACK_CONTENT_SERVICE:
		return enum.MailstackContentService
	case pb.ServiceName_MAILSTACK_EVENT_LOGGER_SERVICE:
		return enum.MailstackEventLoggerService
	default:
		return ""
	}
}
