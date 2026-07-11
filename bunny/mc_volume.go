package bunny

import (
	"context"
	"fmt"
)

func (c *Client) ListVolumes(ctx context.Context, appID string) (*ListVolumesResponse, error) {
	var resp ListVolumesResponse
	err := c.Get(ctx, APIMC, fmt.Sprintf("/apps/%s/volumes", appID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) UpdateVolume(ctx context.Context, appID, volumeName string, size int32) error {
	return c.Patch(ctx, APIMC, fmt.Sprintf("/apps/%s/volumes/%s", appID, volumeName),
		map[string]any{"size": size}, nil)
}

func (c *Client) DetachVolume(ctx context.Context, appID string) error {
	var resp DetachVolumeResponse
	return c.Post(ctx, APIMC, fmt.Sprintf("/apps/%s/volumes/detach", appID), nil, &resp)
}

func (c *Client) DeleteAllVolumeInstances(ctx context.Context, appID string) error {
	return c.Delete(ctx, APIMC, fmt.Sprintf("/apps/%s/volumes", appID), nil)
}

type DetachVolumeResponse struct {
	Name string `json:"name,omitempty"`
}

// Log Forwarding

func (c *Client) CreateLogForwarding(ctx context.Context, cfg LogForwardingConfig) error {
	return c.Post(ctx, APIMC, "/logging", cfg, nil)
}

func (c *Client) ListLogForwarding(ctx context.Context) (*ListLogForwardingResponse, error) {
	var resp ListLogForwardingResponse
	err := c.Get(ctx, APIMC, "/logging", &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) DeleteLogForwarding(ctx context.Context, cfgID string) error {
	return c.Delete(ctx, APIMC, "/logging/"+cfgID, nil)
}
