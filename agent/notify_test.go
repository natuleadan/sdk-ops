package main

import (
	"testing"
)

func TestNewNotifyDispatcherEmpty(t *testing.T) {
	cfg := AgentConfig{}
	nd := newNotifyDispatcher(cfg)
	if nd == nil {
		t.Fatal("newNotifyDispatcher returned nil")
	}
	if len(nd.notifiers) != 0 {
		t.Errorf("expected 0 notifiers, got %d", len(nd.notifiers))
	}
}

func TestNewNotifyDispatcherWithSlack(t *testing.T) {
	cfg := AgentConfig{
		SlackHook: "https://hooks.slack.com/test",
	}
	nd := newNotifyDispatcher(cfg)
	if nd == nil {
		t.Fatal("newNotifyDispatcher returned nil")
	}
	if len(nd.notifiers) != 1 {
		t.Errorf("expected 1 notifier, got %d", len(nd.notifiers))
	}
}

func TestNewNotifyDispatcherWithAll(t *testing.T) {
	cfg := AgentConfig{
		SlackHook:    "https://hooks.slack.com/test",
		DiscordHook:  "https://discord.com/webhook/test",
		TelegramTok:  "bot:token",
		TelegramChat: "-100",
		WebhookURL:   "https://hooks.test.com/webhook",
	}
	nd := newNotifyDispatcher(cfg)
	if len(nd.notifiers) != 4 {
		t.Errorf("expected 4 notifiers, got %d", len(nd.notifiers))
	}
}

func TestNotifyDispatcherSendEmpty(_ *testing.T) {
	cfg := AgentConfig{}
	nd := newNotifyDispatcher(cfg)

	// Send should not panic with no notifiers
	nd.send("test title", "test message")
}

func TestNotifyDispatcherSendWithConfig(t *testing.T) {
	cfg := AgentConfig{
		WebhookURL: "https://httpbin.org/post",
	}
	nd := newNotifyDispatcher(cfg)
	// This will actually send HTTP - skip in short mode
	if testing.Short() {
		t.Skip("skipping HTTP call in short mode")
	}
	nd.send("sdk-ops test", "test notification")
}

func TestNotifyDispatcherTypes(t *testing.T) {
	tests := []struct {
		name string
		cfg  AgentConfig
		want int
	}{
		{"none", AgentConfig{}, 0},
		{"slack", AgentConfig{SlackHook: "https://s.test.com"}, 1},
		{"discord", AgentConfig{DiscordHook: "https://d.test.com"}, 1},
		{"telegram", AgentConfig{TelegramTok: "bot:tok", TelegramChat: "-100"}, 1},
		{"webhook", AgentConfig{WebhookURL: "https://w.test.com"}, 1},
		{"slack+discord", AgentConfig{SlackHook: "https://s.com", DiscordHook: "https://d.com"}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nd := newNotifyDispatcher(tt.cfg)
			if len(nd.notifiers) != tt.want {
				t.Errorf("got %d notifiers, want %d", len(nd.notifiers), tt.want)
			}
		})
	}
}
