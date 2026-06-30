package notify

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSlackSend(t *testing.T) {
	var received bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		w.WriteHeader(200)
	}))
	defer ts.Close()

	n := NewSlack(ts.URL)
	if err := n.Send("test title", "test message"); err != nil {
		t.Fatalf("slack send: %v", err)
	}
	if !received {
		t.Fatal("slack: no request received")
	}
}

func TestDiscordSend(t *testing.T) {
	var received bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		w.WriteHeader(204)
	}))
	defer ts.Close()

	n := NewDiscord(ts.URL)
	if err := n.Send("test", "msg"); err != nil {
		t.Fatalf("discord send: %v", err)
	}
	if !received {
		t.Fatal("discord: no request received")
	}
}

func TestTelegramSend(t *testing.T) {
	n := NewTelegram("test:token", "-100123")
	if n.token != "test:token" || n.chatID != "-100123" {
		t.Fatal("telegram config not set correctly")
	}
}

func TestWebhookSend(t *testing.T) {
	var received bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		w.WriteHeader(200)
	}))
	defer ts.Close()

	n := NewWebhook(ts.URL)
	if err := n.Send("title", "body"); err != nil {
		t.Fatalf("webhook send: %v", err)
	}
	if !received {
		t.Fatal("webhook: no request received")
	}
}

func TestBuildNotifiers(t *testing.T) {
	cfg := Config{
		SlackWebhook:   "https://hooks.slack.com/test",
		DiscordWebhook: "https://discord.com/api/webhooks/test",
		TelegramToken:  "token",
		TelegramChatID: "-100",
		SMTPHost:       "smtp.test.com",
		SMTPPort:       587,
		SMTPUser:       "user",
		SMTPPass:       "pass",
		SMTPFrom:       "from@test.com",
		SMTPTo:         []string{"to@test.com"},
		WebhookURL:     "https://hooks.test.com/webhook",
	}

	nn := BuildNotifiers(cfg)
	if len(nn) != 5 {
		t.Fatalf("expected 5 notifiers, got %d", len(nn))
	}
}

func TestEnabled(t *testing.T) {
	if Enabled(Config{}) {
		t.Fatal("empty config should not be enabled")
	}
	if !Enabled(Config{SlackWebhook: "url"}) {
		t.Fatal("slack config should be enabled")
	}
	if !Enabled(Config{TelegramToken: "token", TelegramChatID: "id"}) {
		t.Fatal("telegram config should be enabled")
	}
}
