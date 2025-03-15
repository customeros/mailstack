package cron_config

type Config struct {
	// Heartbeat check, every minute
	CronScheduleHeartbeat string `env:"CRON_SCHEDULE_HEARTBEAT" envDefault:"0 * * * * *"`
	// Mailstack Reputation Monitoring, daily at midnight
	CronScheduleMailstackReputation string `env:"CRON_SCHEDULE_MAILSTACK_REPUTATION" envDefault:"0 0 0 * * *"`
}
