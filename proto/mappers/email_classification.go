package pb_mappers

import (
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/proto/pb"
)

// EmailClassificationToPb converts an enum.EmailClassification to a pb.EmailClassification
func EmailClassificationToPb(classification enum.EmailClassification) pb.EmailClassification {
	switch classification {
	case enum.EmailAutoResponder:
		return pb.EmailClassification_EMAIL_AUTORESPONDER
	case enum.EmailBounceNotification:
		return pb.EmailClassification_EMAIL_BOUNCE
	case enum.EmailBulk:
		return pb.EmailClassification_EMAIL_BULK
	case enum.EmailInternal:
		return pb.EmailClassification_EMAIL_INTERNAL
	case enum.EmailOK:
		return pb.EmailClassification_EMAIL_OK
	case enum.EmailSensitive:
		return pb.EmailClassification_EMAIL_SENSITIVE
	case enum.EmailSpam:
		return pb.EmailClassification_EMAIL_SPAM
	case enum.EmailWarmer:
		return pb.EmailClassification_EMAIL_WARMER
	default:
		return pb.EmailClassification_EMAIL_CLASSIFICATION_UNKNOWN
	}
}

// PbToEmailClassification converts a pb.EmailClassification to an enum.EmailClassification
func PbToEmailClassification(classification pb.EmailClassification) enum.EmailClassification {
	switch classification {
	case pb.EmailClassification_EMAIL_AUTORESPONDER:
		return enum.EmailAutoResponder
	case pb.EmailClassification_EMAIL_BOUNCE:
		return enum.EmailBounceNotification
	case pb.EmailClassification_EMAIL_BULK:
		return enum.EmailBulk
	case pb.EmailClassification_EMAIL_INTERNAL:
		return enum.EmailInternal
	case pb.EmailClassification_EMAIL_OK:
		return enum.EmailOK
	case pb.EmailClassification_EMAIL_SENSITIVE:
		return enum.EmailSensitive
	case pb.EmailClassification_EMAIL_SPAM:
		return enum.EmailSpam
	case pb.EmailClassification_EMAIL_WARMER:
		return enum.EmailWarmer
	default:
		return ""
	}
}
