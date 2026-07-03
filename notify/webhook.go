package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
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
	req, reqErr := http.NewRequestWithContext(context.Background(), "POST", w.url, bytes.NewReader(body))
	if reqErr != nil {
		return fmt.Errorf("webhook request: %w", reqErr)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook post: %w", err)
	}
	defer func() { if err := resp.Body.Close(); err != nil { log.Printf("webhook: body close error: %v", err) } }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook: HTTP %d", resp.StatusCode)
	}
	return nil
}
