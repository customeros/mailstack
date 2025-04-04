package config

type AppConfig struct {
	Environment         string `env:"ENVIRONMENT,required" envDefault:"dev"`
	APIPort             string `env:"PORT,required" envDefault:"12222"`
	APIKey              string `env:"API_KEY,required"`
	TrackingPublicUrl   string `env:"TRACKING_PUBLIC_URL" envDefault:"https://custosmetrics.com"`
	EMLStorageBucket    string `env:"EML_STORAGE_BUCKET" envDefault:"emails-test"`
	EventsStorageBucket string `env:"EVENTS_STORAGE_BUCKET" envDefailt:"events-test"`
}

type NATSConfig struct {
	Node1 string `env:"NATS_NODE_1_URL,required"`
	Node2 string `env:"NATS_NODE_2_URL"`
	Node3 string `env:"NATS_NODE_3_URL"`
}

type MailstackDatabaseConfig struct {
	Host            string `env:"MAILSTACK_POSTGRES_HOST,required"`
	Port            string `env:"MAILSTACK_POSTGRES_PORT,required"`
	User            string `env:"MAILSTACK_POSTGRES_USER,required"`
	DBName          string `env:"MAILSTACK_POSTGRES_DB_NAME,required"`
	Password        string `env:"MAILSTACK_POSTGRES_PASSWORD,required"`
	MaxConn         int    `env:"MAILSTACK_POSTGRES_DB_MAX_CONN"`
	MaxIdleConn     int    `env:"MAILSTACK_POSTGRES_DB_MAX_IDLE_CONN"`
	ConnMaxLifetime int    `env:"MAILSTACK_POSTGRES_DB_CONN_MAX_LIFETIME"`
	LogLevel        string `env:"MAILSTACK_POSTGRES_LOG_LEVEL" envDefault:"WARN"`
}

type TimescaleDBConfig struct {
	Host            string `env:"TIMESCALE_DB_HOST,required"`
	Port            string `env:"TIMESCALE_DB_PORT,required"`
	User            string `env:"TIMESCALE_DB_USER,required"`
	DBName          string `env:"TIMESCALE_DB_NAME,required"`
	Password        string `env:"TIMESCALE_DB_PASSWORD,required"`
	MaxConn         int    `env:"TIMESCALE_DB_MAX_CONN"`
	MaxIdleConn     int    `env:"TIMESCALE_DB_MAX_IDLE_CONN"`
	ConnMaxLifetime int    `env:"TIMESCALE_DB_CONN_MAX_LIFETIME"`
	LogLevel        string `env:"TIMESCALE_DB_LOG_LEVEL" envDefault:"WARN"`
}

type R2StorageConfig struct {
	AccountID             string `env:"CLOUDFLARE_R2_ACCOUNT_ID,required"`
	AccessKeyID           string `env:"CLOUDFLARE_R2_ACCESS_KEY_ID,required"`
	AccessKeySecret       string `env:"CLOUDFLARE_R2_ACCESS_KEY_SECRET,required"`
	EmailAttachmentBucket string `env:"BUCKET_NAME_EMAIL_ATTACHMENT" envDefault:"attachments"`
}

type DomainConfig struct {
	SupportedTlds []string `env:"MAILSTACK_SUPPORTED_TLD" envDefault:"com"`
}

type NamecheapConfig struct {
	Url                   string  `env:"NAMECHEAP_URL" envDefault:"https://api.namecheap.com/xml.response" validate:"required"`
	ApiKey                string  `env:"NAMECHEAP_API_KEY" `
	ApiUser               string  `env:"NAMECHEAP_API_USER" `
	ApiUsername           string  `env:"NAMECHEAP_API_USERNAME"`
	ApiClientIp           string  `env:"NAMECHEAP_API_CLIENT_IP"`
	MaxPrice              float64 `env:"NAMECHEAP_MAX_PRICE" envDefault:"20.0" `
	Years                 int     `env:"NAMECHEAP_YEARS" envDefault:"1" `
	RegistrantFirstName   string  `env:"NAMECHEAP_REGISTRANT_FIRST_NAME" `
	RegistrantLastName    string  `env:"NAMECHEAP_REGISTRANT_LAST_NAME" `
	RegistrantCompanyName string  `env:"NAMECHEAP_REGISTRANT_COMPANY_NAME" `
	RegistrantJobTitle    string  `env:"NAMECHEAP_REGISTRANT_JOB_TITLE" `
	RegistrantAddress1    string  `env:"NAMECHEAP_REGISTRANT_ADDRESS1" `
	RegistrantCity        string  `env:"NAMECHEAP_REGISTRANT_CITY" `
	RegistrantState       string  `env:"NAMECHEAP_REGISTRANT_STATE" `
	RegistrantZIP         string  `env:"NAMECHEAP_REGISTRANT_ZIP" `
	RegistrantCountry     string  `env:"NAMECHEAP_REGISTRANT_COUNTRY" `
	RegistrantPhoneNumber string  `env:"NAMECHEAP_REGISTRANT_PHONE_NUMBER" `
	RegistrantEmail       string  `env:"NAMECHEAP_REGISTRANT_EMAIL" `
}

type CloudflareConfig struct {
	Url    string `env:"CLOUDFLARE_URL" envDefault:"https://api.cloudflare.com/client/v4" validate:"required"`
	ApiKey string `env:"CLOUDFLARE_API_KEY" `
	Email  string `env:"CLOUDFLARE_API_EMAIL"`
}

type OpenSRSConfig struct {
	Url      string `env:"OPENSRS_URL" envDefault:"https://admin.a.hostedemail.com"`
	ApiKey   string `env:"OPENSRS_API_KEY"`
	Username string `env:"OPENSRS_API_USERNAME"`
}

type CustomerOSAPIConfig struct {
	Url    string `env:"CUSTOMER_OS_API_URL" envDefault:"https://api.customeros.ai" validate:"required"`
	ApiKey string `env:"CUSTOMER_OS_API_KEY"`
}
