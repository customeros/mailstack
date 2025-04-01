package enum

type EmailEvent string

const (
	EventEmailInboundReceivedIMAP            EmailEvent = "emails.inbound.received.imap"
	EventEmailInboundStored                  EmailEvent = "emails.inbound.stored"
	EventEmailInboundClassifiedOK            EmailEvent = "emails.inbound.classified.ok"
	EventEmailInboundClassifiedSkip          EmailEvent = "emails.inbound.classified.skip"
	EventEmailInboundClassifiedBounce        EmailEvent = "emails.inbound.classified.skip"
	EventEmailInboundClassifiedAutoresponder EmailEvent = "emails.inbound.classified.autoresponder"
	EventEmailInboundAnalysis                EmailEvent = "emails.inbound.analysis"
	EventEmailInboundAttachments             EmailEvent = "emails.inbound.attachments"
	EventEmailInboundCompleted               EmailEvent = "emails.inbound.completed"
	EventEmailInboundError                   EmailEvent = "emails.inbound.error"

	EventEmailOutboundScheduled EmailEvent = "emails.outbound.scheduled"
	EventEmailOutboundRequested EmailEvent = "emails.outbound.requested"
	EventEmailOutboundAssembled EmailEvent = "emails.outbound.assembled"
	EventEmailOutboundStored    EmailEvent = "emails.outbound.stored"
	EventEmailOutboundSent      EmailEvent = "emails.outbound.sent"

	EventEmailTrackingClick EmailEvent = "emails.tracking.click"
)

func (e EmailEvent) String() string {
	return string(e)
}
