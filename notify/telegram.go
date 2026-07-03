package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

type Telegram struct {
	token  string
	chatID string
}

func NewTelegram(token, chatID string) *Telegram {
	return &Telegram{token: token, chatID: chatID}
}

func (t *Telegram) Send(title, message string) error {
	text := fmt.Sprintf("*%s*\n%s", title, message)
	payload := map[string]any{
		"chat_id":    t.chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("telegram marshal: %w", err)
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.token)
	req, reqErr := http.NewRequestWithContext(context.Background(), "POST", url, bytes.NewReader(body))
	if reqErr != nil {
		return fmt.Errorf("telegram request: %w", reqErr)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram post: %w", err)
	}
	defer func() { if err := resp.Body.Close(); err != nil { log.Printf("telegram: body close error: %v", err) } }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("telegram: HTTP %d", resp.StatusCode)
	}
	return nil
}
