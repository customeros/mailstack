package config

type GoogleOAuthConfig struct {
	ClientID      string `env:"GOOGLE_OAUTH_CLIENT_ID" envDefault:""`
	ClientSecret  string `env:"GOOGLE_OAUTH_CLIENT_SECRET" envDefault:""`
	EncryptionKey string `env:"EMAIL_ENCRYPTION_KEY" envDefault:""`
}
