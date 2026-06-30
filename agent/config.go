package main

import (
	"os"

	"github.com/natuleadan/sdk-ops/notify"
)

type AgentConfig struct {
	DBPath        string
	APIAddr       string
	Interval      string // metrics collection interval (e.g., "60s")
	Retention     string // data retention (e.g., "720h" = 30 days)
	AutoUpdate    string // "true" to enable auto-update check (default: false)
	UpdateChannel string // "stable" (GitHub releases) or "dev" (latest main)
	SlackHook     string
	DiscordHook   string
	TelegramTok   string
	TelegramChat  string
	SMTPHost      string
	SMTPPort      int
	SMTPUser      string
	SMTPPass      string
	SMTPFrom      string
	SMTPTo        string
	WebhookURL    string
}

func loadConfig() AgentConfig {
	return AgentConfig{
		DBPath:        getEnvDefault("SDK_OPS_AGENT_DB", "/data/sdk-ops-agent.db"),
		APIAddr:       getEnvDefault("SDK_OPS_AGENT_ADDR", ":9000"),
		Interval:      getEnvDefault("SDK_OPS_AGENT_INTERVAL", "60s"),
		Retention:     getEnvDefault("SDK_OPS_AGENT_RETENTION", "720h"),
		AutoUpdate:    os.Getenv("SDK_OPS_AGENT_AUTO_UPDATE"),
		UpdateChannel: getEnvDefault("SDK_OPS_AGENT_UPDATE_CHANNEL", "stable"),
		SlackHook:     os.Getenv("SDK_OPS_SLACK_WEBHOOK"),
		DiscordHook:   os.Getenv("SDK_OPS_DISCORD_WEBHOOK"),
		TelegramTok:   os.Getenv("SDK_OPS_TELEGRAM_TOKEN"),
		TelegramChat:  os.Getenv("SDK_OPS_TELEGRAM_CHAT_ID"),
		SMTPHost:      os.Getenv("SDK_OPS_SMTP_HOST"),
		SMTPPort:      587,
		SMTPUser:      os.Getenv("SDK_OPS_SMTP_USER"),
		SMTPPass:      os.Getenv("SDK_OPS_SMTP_PASS"),
		SMTPFrom:      os.Getenv("SDK_OPS_SMTP_FROM"),
		SMTPTo:        os.Getenv("SDK_OPS_SMTP_TO"),
		WebhookURL:    os.Getenv("SDK_OPS_WEBHOOK_URL"),
	}
}

func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func (c AgentConfig) toNotifyConfig() notify.Config {
	return notify.Config{
		SlackWebhook:   c.SlackHook,
		DiscordWebhook: c.DiscordHook,
		TelegramToken:  c.TelegramTok,
		TelegramChatID: c.TelegramChat,
		SMTPHost:       c.SMTPHost,
		SMTPPort:       c.SMTPPort,
		SMTPUser:       c.SMTPUser,
		SMTPPass:       c.SMTPPass,
		SMTPFrom:       c.SMTPFrom,
		SMTPTo:         []string{c.SMTPTo},
		WebhookURL:     c.WebhookURL,
	}
}
