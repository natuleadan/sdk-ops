package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

type Discord struct {
	webhookURL string
}

func NewDiscord(webhookURL string) *Discord {
	return &Discord{webhookURL: webhookURL}
}

func (d *Discord) Send(title, message string) error {
	payload := map[string]any{
		"content": fmt.Sprintf("**%s**\n%s", title, message),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("discord marshal: %w", err)
	}
	req, reqErr := http.NewRequestWithContext(context.Background(), "POST", d.webhookURL, bytes.NewReader(body))
	if reqErr != nil {
		return fmt.Errorf("discord request: %w", reqErr)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("discord post: %w", err)
	}
	defer func() { if err := resp.Body.Close(); err != nil { log.Printf("discord: body close error: %v", err) } }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("discord: HTTP %d", resp.StatusCode)
	}
	return nil
}
