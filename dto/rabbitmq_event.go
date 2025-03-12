package dto

import "github.com/customeros/mailstack/internal/enum"

type Event struct {
	Event    EventDetails  `json:"event"`
	Metadata EventMetadata `json:"metadata"`
}

type EventDetails struct {
	Id         string          `json:"id"`
	Tenant     string          `json:"tenant"`
	EntityId   string          `json:"entityId"`
	EntityType enum.EntityType `json:"entityType"`
	EventType  string          `json:"eventType"`
	Data       interface{}     `json:"data"`
}

type EventMetadata struct {
	UberTraceId string `json:"uber-trace-id"`
	AppSource   string `json:"appSource"`
	UserId      string `json:"userId"`
	UserEmail   string `json:"userEmail"`
	Timestamp   string `json:"timestamp"`
}
