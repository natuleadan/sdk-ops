package notify

import "fmt"

type Notifier interface {
	Send(title, message string) error
}

type Config struct {
	SlackWebhook   string
	DiscordWebhook string
	TelegramToken  string
	TelegramChatID string
	SMTPHost       string
	SMTPPort       int
	SMTPUser       string
	SMTPPass       string
	SMTPFrom       string
	SMTPTo         []string
	WebhookURL     string
}

func Enabled(cfg Config) bool {
	return cfg.SlackWebhook != "" ||
		cfg.DiscordWebhook != "" ||
		cfg.TelegramToken != "" ||
		cfg.SMTPHost != "" ||
		cfg.WebhookURL != ""
}

func BuildNotifiers(cfg Config) []Notifier {
	var nn []Notifier
	if cfg.SlackWebhook != "" {
		nn = append(nn, NewSlack(cfg.SlackWebhook))
	}
	if cfg.DiscordWebhook != "" {
		nn = append(nn, NewDiscord(cfg.DiscordWebhook))
	}
	if cfg.TelegramToken != "" {
		nn = append(nn, NewTelegram(cfg.TelegramToken, cfg.TelegramChatID))
	}
	if cfg.SMTPHost != "" {
		nn = append(nn, NewSMTP(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPFrom, cfg.SMTPTo))
	}
	if cfg.WebhookURL != "" {
		nn = append(nn, NewWebhook(cfg.WebhookURL))
	}
	return nn
}

func SendAll(nn []Notifier, title, message string) []error {
	var errs []error
	for _, n := range nn {
		if err := n.Send(title, message); err != nil {
			errs = append(errs, fmt.Errorf("%T: %w", n, err))
		}
	}
	return errs
}


