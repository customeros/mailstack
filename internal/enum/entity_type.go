package enum

type EntityType string

const (
	EMAIL_SIGNATURE EntityType = "EMAIL_SIGNATURE"
	RAW_EMAIL       EntityType = "RAW_EMAIL"
)

func (entityType EntityType) String() string {
	return string(entityType)
}

func GetEntityType(s string) EntityType {
	return EntityType(s)
}
