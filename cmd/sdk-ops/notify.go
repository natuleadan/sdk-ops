package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/natuleadan/sdk-ops/notify"
)

func newNotifyCmd() *cobra.Command {
	var cfg notify.Config

	cmd := &cobra.Command{
		Use:   "notify",
		Short: "Send notifications to Slack, Discord, Telegram, email, or webhooks",
	}

	sendCmd := &cobra.Command{
		Use:   "send <title> <message>",
		Short: "Send a notification message",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			title, message := args[0], args[1]

			nn := notify.BuildNotifiers(cfg)
			if len(nn) == 0 {
				return fmt.Errorf("no notifiers configured. Provide at least one flag (--slack, --discord, --telegram, --email, --webhook)")
			}

			errs := notify.SendAll(nn, title, message)
			for _, err := range errs {
				fmt.Fprintf(cmd.ErrOrStderr(), "  ⚠️  %v\n", err)
			}

			success := len(nn) - len(errs)
			fmt.Printf("  → Sent to %d/%d notifiers\n", success, len(nn))
			return nil
		},
	}

	testCmd := &cobra.Command{
		Use:   "test",
		Short: "Test all configured notifiers",
		RunE: func(cmd *cobra.Command, args []string) error {
			nn := notify.BuildNotifiers(cfg)
			if len(nn) == 0 {
				return fmt.Errorf("no notifiers configured")
			}

			title := "sdk-ops test notification"
			message := "If you see this, notifications are working correctly."

			errs := notify.SendAll(nn, title, message)
			for _, err := range errs {
				fmt.Fprintf(cmd.ErrOrStderr(), "  ⚠️  %v\n", err)
			}

			success := len(nn) - len(errs)
			fmt.Printf("  → %d/%d notifiers responded OK\n", success, len(nn))
			if len(errs) > 0 {
				return fmt.Errorf("%d notifier(s) failed", len(errs))
			}
			return nil
		},
	}

	sendCmd.Flags().StringVar(&cfg.SlackWebhook, "slack", "", "Slack webhook URL")
	sendCmd.Flags().StringVar(&cfg.DiscordWebhook, "discord", "", "Discord webhook URL")
	sendCmd.Flags().StringVar(&cfg.TelegramToken, "telegram", "", "Telegram bot token")
	sendCmd.Flags().StringVar(&cfg.TelegramChatID, "chat-id", "", "Telegram chat ID")
	sendCmd.Flags().StringVar(&cfg.SMTPHost, "email", "", "SMTP host (e.g., smtp.gmail.com)")
	sendCmd.Flags().IntVar(&cfg.SMTPPort, "email-port", 587, "SMTP port")
	sendCmd.Flags().StringVar(&cfg.SMTPUser, "email-user", "", "SMTP user")
	sendCmd.Flags().StringVar(&cfg.SMTPPass, "email-pass", "", "SMTP password")
	sendCmd.Flags().StringVar(&cfg.SMTPFrom, "email-from", "", "SMTP from address")
	sendCmd.Flags().StringArrayVar(&cfg.SMTPTo, "email-to", nil, "SMTP recipient (can be repeated)")
	sendCmd.Flags().StringVar(&cfg.WebhookURL, "webhook", "", "Custom webhook URL")

	testCmd.Flags().AddFlagSet(sendCmd.Flags())

	cmd.AddCommand(sendCmd)
	cmd.AddCommand(testCmd)
	return cmd
}


