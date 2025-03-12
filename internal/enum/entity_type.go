package enum

type EntityType string

const (
	AGENT                    EntityType = "AGENT"
	AGENT_EXECUTION          EntityType = "AGENT_EXECUTION"
	AUTHENTICATION_USER      EntityType = "AUTHENTICATION_USER"
	ATTACHMENT               EntityType = "ATTACHMENT"
	COMMENT                  EntityType = "COMMENT"
	CONTACT                  EntityType = "CONTACT"
	CONTRACT                 EntityType = "CONTRACT"
	CUSTOM_FIELD             EntityType = "CUSTOM_FIELD"
	CUSTOM_FIELD_TEMPLATE    EntityType = "CUSTOM_FIELD_TEMPLATE"
	DOMAIN                   EntityType = "DOMAIN"
	EMAIL                    EntityType = "EMAIL"
	FLOW                     EntityType = "FLOW"
	FLOW_ACTION              EntityType = "FLOW_ACTION"
	FLOW_ACTION_EVENT        EntityType = "FLOW_ACTION_EVENT"
	FLOW_ACTION_RESULT_EVENT EntityType = "FLOW_ACTION_RESULT_EVENT"
	FLOW_PARTICIPANT         EntityType = "FLOW_PARTICIPANT"
	FLOW_SENDER              EntityType = "FLOW_SENDER"
	INTENT_SIGNAL            EntityType = "INTENT_SIGNAL"
	INTERACTION_EVENT        EntityType = "INTERACTION_EVENT"
	INTERACTION_SESSION      EntityType = "INTERACTION_SESSION"
	INVOICE                  EntityType = "INVOICE"
	ISSUE                    EntityType = "ISSUE"
	LOG_ENTRY                EntityType = "LOG_ENTRY"
	MAILBOX                  EntityType = "MAILBOX"
	MAILSTACK_BUY_REQUEST    EntityType = "MAILSTACK_BUY_REQUEST"
	MARKDOWN_EVENT           EntityType = "MARKDOWN_EVENT"
	MEETING                  EntityType = "MEETING"
	NOTE                     EntityType = "NOTE"
	OPPORTUNITY              EntityType = "OPPORTUNITY"
	ORGANIZATION             EntityType = "ORGANIZATION"
	PHONE_NUMBER             EntityType = "PHONE_NUMBER"
	REMINDER                 EntityType = "REMINDER"
	SERVICE_LINE_ITEM        EntityType = "SERVICE_LINE_ITEM"
	SOCIAL                   EntityType = "SOCIAL"
	TAG                      EntityType = "TAG"
	TASK                     EntityType = "TASK"
	TENANT                   EntityType = "TENANT"
	TENANT_SETTINGS          EntityType = "TENANT_SETTINGS"
	USER                     EntityType = "USER"
	LOCATION                 EntityType = "LOCATION"
	JOB_ROLE                 EntityType = "JOB_ROLE"
	SKU                      EntityType = "SKU"
	WEB_SESSION              EntityType = "WEB_SESSION"
	INGEST_EMAIL_MESSAGE     EntityType = "INGEST_EMAIL_MESSAGE"
)

func (entityType EntityType) String() string {
	return string(entityType)
}

func GetEntityType(s string) EntityType {
	return EntityType(s)
}
