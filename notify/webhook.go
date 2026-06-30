package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

type Webhook struct {
	url string
}

func NewWebhook(url string) *Webhook {
	return &Webhook{url: url}
}

func (w *Webhook) Send(title, message string) error {
	payload := map[string]string{
		"title":   title,
		"message": message,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("webhook marshal: %w", err)
	}
	resp, err := http.Post(w.url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook: HTTP %d", resp.StatusCode)
	}
	return nil
}
