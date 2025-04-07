package enum

type MailstackService string

const (
	MailstackIMAPService           MailstackService = "mailstack.imap_service"
	MailstackStorageService        MailstackService = "mailstack.email_storage_service"
	MailstackClassificationService MailstackService = "mailstack.email_classification_service"
	MailstackAnalysisService       MailstackService = "mailstack.email_analysis_service"
	MailstackAttachmentsService    MailstackService = "mailstack.attachments_service"
	MailstackThreadingService      MailstackService = "mailstack.email_threading_service"
	MailstackContentService        MailstackService = "mailstack.email_content_service"
	MailstackEventLoggerService    MailstackService = "mailstack.event_logger_service"
)

func (e MailstackService) String() string {
	return string(e)
}
