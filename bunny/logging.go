package bunny

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type CDNLogQuery struct {
	PullZoneID        int64     `json:"pullZoneId"`
	From              time.Time `json:"from"`
	To                time.Time `json:"to"`
	Status            string    `json:"status,omitempty"`
	CacheStatus       string    `json:"cacheStatus,omitempty"`
	Country           string    `json:"country,omitempty"`
	EdgeLocation      string    `json:"edgeLocation,omitempty"`
	RemoteIP          string    `json:"remoteIp,omitempty"`
	URLContains       string    `json:"urlContains,omitempty"`
	UserAgentContains string    `json:"userAgentContains,omitempty"`
	RefererContains   string    `json:"refererContains,omitempty"`
	Search            string    `json:"search,omitempty"`
	RequestID         string    `json:"requestId,omitempty"`
	Limit             int       `json:"limit,omitempty"`
	Offset            int       `json:"offset,omitempty"`
	Order             string    `json:"order,omitempty"` // asc or desc
}

type CDNLogEntry struct {
	Timestamp          string `json:"timestamp"`
	PullZoneID         int64  `json:"pullZoneId"`
	RequestID          string `json:"requestId"`
	CacheStatus        string `json:"cacheStatus"`
	StatusCode         int    `json:"statusCode"`
	BytesSent          int64  `json:"bytesSent"`
	RemoteIP           string `json:"remoteIp,omitempty"`
	CountryCode        string `json:"countryCode,omitempty"`
	EdgeLocation       string `json:"edgeLocation,omitempty"`
	Scheme             string `json:"scheme"`
	Host               string `json:"host"`
	Path               string `json:"path"`
	URL                string `json:"url"`
	UserAgent          string `json:"userAgent,omitempty"`
	Referer            string `json:"referer,omitempty"`
	BodyBytesSent      int64  `json:"bodyBytesSent,omitempty"`
	ContentRange       string `json:"contentRange,omitempty"`
	AuthorizationHeader string `json:"authorizationHeader,omitempty"`
}

type CDNLogResponse struct {
	Data       []CDNLogEntry  `json:"data"`
	Pagination PaginationInfo `json:"pagination"`
	Query      QuerySummary   `json:"query"`
}

type PaginationInfo struct {
	Offset   int   `json:"offset"`
	Limit    int   `json:"limit"`
	Returned int   `json:"returned"`
	HasMore  bool  `json:"hasMore"`
}

type QuerySummary struct {
	PullZoneID int64  `json:"pullZoneId"`
	From       string `json:"from"`
	To         string `json:"to"`
	Order      string `json:"order"`
}

func (c *Client) QueryCDNLogs(ctx context.Context, pullZoneID int64, from, to time.Time, opts CDNLogQuery) (*CDNLogResponse, error) {
	u := fmt.Sprintf("%s/v2/pullzones/%d/logs?from=%s&to=%s",
		LoggingAPIBase, pullZoneID,
		from.Format(time.RFC3339), to.Format(time.RFC3339))
	if opts.Status != "" {
		u += "&status=" + opts.Status
	}
	if opts.Limit > 0 {
		u += fmt.Sprintf("&limit=%d", opts.Limit)
	}
	if opts.Order != "" {
		u += "&order=" + opts.Order
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("cdn logs request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("AccessKey", c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cdn logs do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("cdn logs: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var result CDNLogResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("cdn logs decode: %w", err)
	}
	return &result, nil
}
