package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

type Slack struct {
	webhookURL string
}

func NewSlack(webhookURL string) *Slack {
	return &Slack{webhookURL: webhookURL}
}

func (s *Slack) Send(title, message string) error {
	payload := map[string]any{
		"text": fmt.Sprintf("*%s*\n%s", title, message),
	}
	return s.post(payload)
}

func (s *Slack) post(payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("slack marshal: %w", err)
	}
	req, reqErr := http.NewRequestWithContext(context.Background(), "POST", s.webhookURL, bytes.NewReader(body))
	if reqErr != nil {
		return fmt.Errorf("slack request: %w", reqErr)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("slack post: %w", err)
	}
	defer func() { if err := resp.Body.Close(); err != nil { log.Printf("slack: body close error: %v", err) } }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("slack: HTTP %d", resp.StatusCode)
	}
	return nil
}
