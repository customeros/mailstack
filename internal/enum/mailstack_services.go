package enum

type MailstackService string

const (
	MailstackStorageService        MailstackService = "mailstack.email_storage_service"
	MailstackClassificationService MailstackService = "mailstack.email_classification_service"
)

func (e MailstackService) String() string {
	return string(e)
}
