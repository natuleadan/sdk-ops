package bunny

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// --- Storage Zones (from Core API) ---

type StorageZone struct {
	ID                 int64    `json:"Id"`
	UserID             string   `json:"UserId,omitempty"`
	Name               string   `json:"Name"`
	Password           string   `json:"Password,omitempty"`
	ReadOnlyPassword   string   `json:"ReadOnlyPassword,omitempty"`
	StorageUsed        int64    `json:"StorageUsed,omitempty"`
	FilesStored        int64    `json:"FilesStored,omitempty"`
	Region             string   `json:"Region,omitempty"`
	ReplicationRegions []string `json:"ReplicationRegions,omitempty"`
	StorageHostname    string   `json:"StorageHostname,omitempty"`
	ZoneTier           int      `json:"ZoneTier,omitempty"`
}

type StorageZoneRegion struct {
	ID         int     `json:"Id"`
	Name       string  `json:"Name"`
	PricePerGB float64 `json:"PricePerGB,omitempty"`
}

type AddStorageZoneModel struct {
	Name   string `json:"Name"`
	Region string `json:"Region,omitempty"`
}

// --- File Operations ---

type StorageFile struct {
	ObjectName      string `json:"ObjectName"`
	Path            string `json:"Path"`
	IsDirectory     bool   `json:"IsDirectory"`
	Size            int64  `json:"Size"`
	LastChanged     string `json:"LastChanged"`
	ServerID        int    `json:"ServerId"`
	UserID          string `json:"UserId"`
	ContentType     string `json:"ContentType"`
	StorageZoneID   int64  `json:"StorageZoneId"`
	StorageZoneName string `json:"StorageZoneName"`
}

// --- Storage Zone CRUD ---

func (c *Client) ListStorageZones(ctx context.Context) ([]StorageZone, error) {
	var zones []StorageZone
	err := c.Get(ctx, APICore, "/storagezone", &zones)
	if err != nil {
		return nil, err
	}
	return zones, nil
}

func (c *Client) CreateStorageZone(ctx context.Context, req AddStorageZoneModel) (*StorageZone, error) {
	var zone StorageZone
	err := c.Post(ctx, APICore, "/storagezone", req, &zone)
	if err != nil {
		return nil, err
	}
	return &zone, nil
}

func (c *Client) GetStorageZone(ctx context.Context, id int64) (*StorageZone, error) {
	var zone StorageZone
	err := c.Get(ctx, APICore, fmt.Sprintf("/storagezone/%d", id), &zone)
	if err != nil {
		return nil, err
	}
	return &zone, nil
}

func (c *Client) DeleteStorageZone(ctx context.Context, id int64) error {
	return c.Delete(ctx, APICore, fmt.Sprintf("/storagezone/%d", id), nil)
}

func (c *Client) ResetStoragePassword(ctx context.Context, id int64) (*StorageZone, error) {
	var zone StorageZone
	err := c.Post(ctx, APICore, fmt.Sprintf("/storagezone/%d/resetPassword", id), nil, &zone)
	if err != nil {
		return nil, err
	}
	return &zone, nil
}

// --- File Operations (raw binary, no multipart) ---

func (c *Client) storageURL(zoneName, path string) string {
	return fmt.Sprintf("https://storage.bunnycdn.com/%s/%s", zoneName, strings.TrimPrefix(path, "/"))
}

func (c *Client) ListStorageFiles(ctx context.Context, zoneName, path, accessKey string) ([]StorageFile, error) {
	u := c.storageURL(zoneName, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("storage list: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("AccessKey", accessKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("storage list: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("storage list: HTTP %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("storage list read: %w", err)
	}

	var files []StorageFile
	if err := json.Unmarshal(body, &files); err != nil {
		return nil, fmt.Errorf("storage list decode: %w", err)
	}
	return files, nil
}

func (c *Client) UploadStorageFile(ctx context.Context, zoneName, path, fileName string, content []byte, accessKey string) error {
	fullPath := strings.TrimSuffix(path, "/") + "/" + fileName
	u := c.storageURL(zoneName, fullPath)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(content))
	if err != nil {
		return fmt.Errorf("storage upload: %w", err)
	}
	req.Header.Set("AccessKey", accessKey)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("storage upload: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 201 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("storage upload: HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (c *Client) DownloadStorageFile(ctx context.Context, zoneName, filePath, accessKey string) ([]byte, error) {
	u := c.storageURL(zoneName, filePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("storage download: %w", err)
	}
	req.Header.Set("AccessKey", accessKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("storage download: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("storage download: HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func (c *Client) DeleteStorageFile(ctx context.Context, zoneName, filePath, accessKey string) error {
	u := c.storageURL(zoneName, filePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return fmt.Errorf("storage delete: %w", err)
	}
	req.Header.Set("AccessKey", accessKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("storage delete: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("storage delete: HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
