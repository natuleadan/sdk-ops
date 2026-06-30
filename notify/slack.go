package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

type Slack struct {
	webhookURL string
}

func NewSlack(webhookURL string) *Slack {
	return &Slack{webhookURL: webhookURL}
}

func (s *Slack) Send(title, message string) error {
	payload := map[string]interface{}{
		"text": fmt.Sprintf("*%s*\n%s", title, message),
	}
	return s.post(payload)
}

func (s *Slack) post(payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("slack marshal: %w", err)
	}
	resp, err := http.Post(s.webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("slack post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("slack: HTTP %d", resp.StatusCode)
	}
	return nil
}
