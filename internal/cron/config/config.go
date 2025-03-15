package cron_config

type Config struct {
	// Mailstack Reputation Monitoring, daily at midnight
	CronScheduleMailstackReputation string `env:"CRON_SCHEDULE_MAILSTACK_REPUTATION" envDefault:"0 0 * * *"`
}
