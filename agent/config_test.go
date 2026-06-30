package main

import (
	"os"
	"testing"
)

func TestLoadConfigDefaults(t *testing.T) {
	// Clear relevant env vars
	for _, key := range []string{
		"SDK_OPS_AGENT_DB", "SDK_OPS_AGENT_ADDR", "SDK_OPS_AGENT_INTERVAL",
		"SDK_OPS_SLACK_WEBHOOK", "SDK_OPS_DISCORD_WEBHOOK",
		"SDK_OPS_TELEGRAM_TOKEN", "SDK_OPS_TELEGRAM_CHAT_ID",
	} {
		os.Unsetenv(key)
	}

	cfg := loadConfig()

	if cfg.DBPath != "/data/sdk-ops-agent.db" {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, "/data/sdk-ops-agent.db")
	}
	if cfg.APIAddr != ":9000" {
		t.Errorf("APIAddr = %q, want %q", cfg.APIAddr, ":9000")
	}
	if cfg.Interval != "60s" {
		t.Errorf("Interval = %q, want %q", cfg.Interval, "60s")
	}
	if cfg.Retention != "720h" {
		t.Errorf("Retention = %q, want %q", cfg.Retention, "720h")
	}
	if cfg.SlackHook != "" {
		t.Errorf("SlackHook = %q, want empty", cfg.SlackHook)
	}
}

func TestLoadConfigCustom(t *testing.T) {
	os.Setenv("SDK_OPS_AGENT_DB", "/custom/path/db.sqlite")
	os.Setenv("SDK_OPS_AGENT_ADDR", ":9090")
	os.Setenv("SDK_OPS_AGENT_INTERVAL", "30s")
	os.Setenv("SDK_OPS_AGENT_RETENTION", "168h")
	os.Setenv("SDK_OPS_SLACK_WEBHOOK", "https://hooks.slack.com/test")
	os.Setenv("SDK_OPS_DISCORD_WEBHOOK", "https://discord.com/webhook/test")
	os.Setenv("SDK_OPS_TELEGRAM_TOKEN", "bot123:abc")
	os.Setenv("SDK_OPS_TELEGRAM_CHAT_ID", "-100123")
	defer func() {
		os.Unsetenv("SDK_OPS_AGENT_DB")
		os.Unsetenv("SDK_OPS_AGENT_ADDR")
		os.Unsetenv("SDK_OPS_AGENT_INTERVAL")
		os.Unsetenv("SDK_OPS_AGENT_RETENTION")
		os.Unsetenv("SDK_OPS_SLACK_WEBHOOK")
		os.Unsetenv("SDK_OPS_DISCORD_WEBHOOK")
		os.Unsetenv("SDK_OPS_TELEGRAM_TOKEN")
		os.Unsetenv("SDK_OPS_TELEGRAM_CHAT_ID")
	}()

	cfg := loadConfig()

	if cfg.DBPath != "/custom/path/db.sqlite" {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, "/custom/path/db.sqlite")
	}
	if cfg.APIAddr != ":9090" {
		t.Errorf("APIAddr = %q, want %q", cfg.APIAddr, ":9090")
	}
	if cfg.Interval != "30s" {
		t.Errorf("Interval = %q, want %q", cfg.Interval, "30s")
	}
	if cfg.Retention != "168h" {
		t.Errorf("Retention = %q, want %q", cfg.Retention, "168h")
	}
	if cfg.SlackHook != "https://hooks.slack.com/test" {
		t.Errorf("SlackHook = %q, want %q", cfg.SlackHook, "https://hooks.slack.com/test")
	}
	if cfg.TelegramTok != "bot123:abc" {
		t.Errorf("TelegramTok = %q, want %q", cfg.TelegramTok, "bot123:abc")
	}
	if cfg.TelegramChat != "-100123" {
		t.Errorf("TelegramChat = %q, want %q", cfg.TelegramChat, "-100123")
	}
}

func TestGetEnvDefault(t *testing.T) {
	if got := getEnvDefault("SDK_OPS_NONEXISTENT", "fallback"); got != "fallback" {
		t.Errorf("getEnvDefault = %q, want %q", got, "fallback")
	}

	os.Setenv("SDK_OPS_TEST_EXISTS", "custom")
	defer os.Unsetenv("SDK_OPS_TEST_EXISTS")

	if got := getEnvDefault("SDK_OPS_TEST_EXISTS", "fallback"); got != "custom" {
		t.Errorf("getEnvDefault = %q, want %q", got, "custom")
	}
}

func TestToNotifyConfig(t *testing.T) {
	cfg := AgentConfig{
		SlackHook:    "https://hooks.slack.com/test",
		DiscordHook:  "https://discord.com/webhook/test",
		TelegramTok:  "bot:token",
		TelegramChat: "-100",
		SMTPHost:     "smtp.test.com",
		SMTPPort:     587,
		SMTPUser:     "user",
		SMTPPass:     "pass",
		SMTPFrom:     "from@test.com",
		SMTPTo:       "to@test.com",
		WebhookURL:   "https://hook.test.com",
	}

	nc := cfg.toNotifyConfig()

	if nc.SlackWebhook != "https://hooks.slack.com/test" {
		t.Errorf("SlackWebhook = %q", nc.SlackWebhook)
	}
	if nc.DiscordWebhook != "https://discord.com/webhook/test" {
		t.Errorf("DiscordWebhook = %q", nc.DiscordWebhook)
	}
	if nc.TelegramToken != "bot:token" {
		t.Errorf("TelegramToken = %q", nc.TelegramToken)
	}
	if nc.SMTPHost != "smtp.test.com" {
		t.Errorf("SMTPHost = %q", nc.SMTPHost)
	}
	if len(nc.SMTPTo) != 1 || nc.SMTPTo[0] != "to@test.com" {
		t.Errorf("SMTPTo = %v", nc.SMTPTo)
	}
}

func TestEmptyNotifyConfig(t *testing.T) {
	cfg := AgentConfig{}
	nc := cfg.toNotifyConfig()

	if nc.SlackWebhook != "" {
		t.Error("SlackWebhook should be empty")
	}
	if nc.TelegramToken != "" {
		t.Error("TelegramToken should be empty")
	}
}
