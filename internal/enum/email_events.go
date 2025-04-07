package enum

type EmailEvent string

const (
	EventEmailInboundReceivedIMAP            EmailEvent = "emails.inbound.received.imap"
	EventEmailInboundStored                  EmailEvent = "emails.inbound.stored"
	EventEmailInboundClassify                EmailEvent = "emails.inbound.classify"
	EventEmailInboundClassifiedOK            EmailEvent = "emails.inbound.classified.ok"
	EventEmailInboundClassifiedSkip          EmailEvent = "emails.inbound.classified.skip"
	EventEmailInboundClassifiedBounce        EmailEvent = "emails.inbound.classified.bounce"
	EventEmailInboundClassifiedAutoresponder EmailEvent = "emails.inbound.classified.autoresponder"
	EventEmailInboundAnalysis                EmailEvent = "emails.inbound.analysis"
	EventEmailInboundAttachments             EmailEvent = "emails.inbound.attachments"
	EventEmailInboundThread                  EmailEvent = "emails.inbound.thread"
	EventEmailInboundCompleted               EmailEvent = "emails.inbound.completed"

	EventEmailOutboundScheduled EmailEvent = "emails.outbound.scheduled"
	EventEmailOutboundRequested EmailEvent = "emails.outbound.requested"
	EventEmailOutboundAssembled EmailEvent = "emails.outbound.assembled"
	EventEmailOutboundStored    EmailEvent = "emails.outbound.stored"
	EventEmailOutboundSent      EmailEvent = "emails.outbound.sent"

	EventEmailErrorInbound  EmailEvent = "emails.errors.inbound"
	EventEmailErrorOutbound EmailEvent = "emails.errors.outbound"
	EventEmailErrorLogger   EmailEvent = "emails.errors.logger"

	EventEmailTrackingClick EmailEvent = "emails.tracking.click"
)

func (e EmailEvent) String() string {
	return string(e)
}
