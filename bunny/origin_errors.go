package bunny

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type OriginErrorLog struct {
	Date      string `json:"Date,omitempty"`
	PullZoneID int64  `json:"PullZoneId,omitempty"`
	Entries   []OriginErrorEntry `json:"Entries,omitempty"`
}

type OriginErrorEntry struct {
	Timestamp   string `json:"Timestamp,omitempty"`
	StatusCode  int    `json:"StatusCode,omitempty"`
	Error       string `json:"Error,omitempty"`
	Path        string `json:"Path,omitempty"`
	EdgeLocation string `json:"EdgeLocation,omitempty"`
}

func (c *Client) GetOriginErrors(ctx context.Context, pullZoneID int64, date time.Time) ([]OriginErrorEntry, error) {
	dateStr := date.Format("2006-01-02")
	u := fmt.Sprintf("https://api.bunny.net/origin-error/%d/%s", pullZoneID, dateStr)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("origin-errors request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("AccessKey", c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("origin-errors do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("origin-errors: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var logs struct {
		Data []OriginErrorEntry `json:"data,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&logs); err != nil {
		return nil, fmt.Errorf("origin-errors decode: %w", err)
	}
	return logs.Data, nil
}
