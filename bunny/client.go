package bunny

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	CoreAPIBase   = "https://api.bunny.net"
	MCAPIBase     = "https://api.bunny.net/mc"
	LoggingAPIBase = "https://logging.bunnycdn.com"
	VideoAPIBase  = "https://video.bunnycdn.com"
	ShieldAPIBase = "https://api.bunny.net/shield"
	StorageAPIBase = "https://storage.bunnycdn.com"
)

type Client struct {
	apiKey     string
	httpClient *http.Client
	baseURLs   map[string]string
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        25,
				IdleConnTimeout:     90 * time.Second,
				DisableCompression:  false,
			},
		},
		baseURLs: map[string]string{
			"core":    CoreAPIBase,
			"mc":      MCAPIBase,
			"logging": LoggingAPIBase,
			"video":   VideoAPIBase,
			"shield":  ShieldAPIBase,
			"storage": StorageAPIBase,
		},
	}
}

func (c *Client) baseURL(api string) string {
	if u, ok := c.baseURLs[api]; ok {
		return u
	}
	return CoreAPIBase
}

func (c *Client) Do(ctx context.Context, method, api, path string, body, dst any) error {
	base := c.baseURL(api)

	var query string
	if idx := strings.IndexByte(path, '?'); idx >= 0 {
		query = path[idx:]
		path = path[:idx]
	}

	finalURL := base + path
	if query != "" {
		finalURL += query
	}

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("bunny: marshal body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, finalURL, reqBody)
	if err != nil {
		return fmt.Errorf("bunny: create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("AccessKey", c.apiKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("bunny: do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &APIError{
			Status:  resp.StatusCode,
			Message: string(respBody),
		}
	}

	if dst != nil {
		if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
			return fmt.Errorf("bunny: decode response: %w", err)
		}
	}

	return nil
}

func (c *Client) Get(ctx context.Context, api, path string, dst any) error {
	return c.Do(ctx, http.MethodGet, api, path, nil, dst)
}

func (c *Client) Post(ctx context.Context, api, path string, body, dst any) error {
	return c.Do(ctx, http.MethodPost, api, path, body, dst)
}

func (c *Client) Put(ctx context.Context, api, path string, body, dst any) error {
	return c.Do(ctx, http.MethodPut, api, path, body, dst)
}

func (c *Client) Patch(ctx context.Context, api, path string, body, dst any) error {
	return c.Do(ctx, http.MethodPatch, api, path, body, dst)
}

func (c *Client) Delete(ctx context.Context, api, path string, dst any) error {
	return c.Do(ctx, http.MethodDelete, api, path, nil, dst)
}

type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("bunny: api error %d: %s", e.Status, e.Message)
}
