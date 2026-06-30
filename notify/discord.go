package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

type Discord struct {
	webhookURL string
}

func NewDiscord(webhookURL string) *Discord {
	return &Discord{webhookURL: webhookURL}
}

func (d *Discord) Send(title, message string) error {
	payload := map[string]interface{}{
		"content": fmt.Sprintf("**%s**\n%s", title, message),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("discord marshal: %w", err)
	}
	resp, err := http.Post(d.webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("discord post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("discord: HTTP %d", resp.StatusCode)
	}
	return nil
}
