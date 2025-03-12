package enum

type EmailProvider string

const (
	EmailGoogleWorkspace EmailProvider = "google_workspace"
	EmailOutlook         EmailProvider = "outlook"
	EmailMailstack       EmailProvider = "mailstack"
	EmailGeneric         EmailProvider = "generic"
)

func (t EmailProvider) String() string {
	return string(t)
}

type EmailClassification string

const (
	EmailAutoResponder      EmailClassification = "auto_responder"
	EmailBounceNotification EmailClassification = "bounce_notification"
	EmailBulk               EmailClassification = "bulk_email"
	EmailInternal           EmailClassification = "internal"
	EmailOK                 EmailClassification = "ok"
	EmailSensitive          EmailClassification = "sensitive"
	EmailSpam               EmailClassification = "spam"
	EmailWarmer             EmailClassification = "email_warmer"
)

func (t EmailClassification) String() string {
	return string(t)
}

type EmailDirection string

const (
	EmailInbound  EmailDirection = "inbound"
	EmailOutbound EmailDirection = "outbound"
)

func (t EmailDirection) String() string {
	return string(t)
}

type EmailStatus string

const (
	EmailStatusReceived  EmailStatus = "received"
	EmailStatusDraft     EmailStatus = "draft"
	EmailStatusScheduled EmailStatus = "scheduled"
	EmailStatusSent      EmailStatus = "sent"
	EmailStatusFailed    EmailStatus = "failed"
	EmailStatusBounced   EmailStatus = "bounced"
)

func (t EmailStatus) String() string {
	return string(t)
}

type EmailSecurity string

const (
	EmailSecurityNone     EmailSecurity = "none"
	EmailSecuritySSL      EmailSecurity = "ssl"
	EmailSecurityTLS      EmailSecurity = "tls"
	EmailSecurityStartTLS EmailSecurity = "startTLS"
)

func (t EmailSecurity) String() string {
	return string(t)
}
