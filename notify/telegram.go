package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	payload := map[string]interface{}{
		"chat_id":    t.chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("telegram marshal: %w", err)
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.token)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("telegram: HTTP %d", resp.StatusCode)
	}
	return nil
}
