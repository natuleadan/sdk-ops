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

func (c *Client) DetachVolume(ctx context.Context, appID, volumeID string) error {
	var resp DetachVolumeResponse
	return c.Post(ctx, APIMC, fmt.Sprintf("/apps/%s/volumes/%s/detach", appID, volumeID), nil, &resp)
}

func (c *Client) DeleteVolumeInstance(ctx context.Context, appID, volumeID, instanceID string) error {
	return c.Delete(ctx, APIMC, fmt.Sprintf("/apps/%s/volumes/%s/instances/%s", appID, volumeID, instanceID), nil)
}

func (c *Client) DeleteAllVolumeInstances(ctx context.Context, appID, volumeID string) error {
	return c.Delete(ctx, APIMC, fmt.Sprintf("/apps/%s/volumes/%s", appID, volumeID), nil)
}

type DetachVolumeResponse struct {
	Name string `json:"name,omitempty"`
}

// Log Forwarding

func (c *Client) CreateLogForwarding(ctx context.Context, cfg LogForwardingConfig) error {
	return c.Post(ctx, APIMC, "/log/forwarding", cfg, nil)
}

func (c *Client) ListLogForwarding(ctx context.Context) (*ListLogForwardingResponse, error) {
	var resp ListLogForwardingResponse
	err := c.Get(ctx, APIMC, "/log/forwarding", &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetLogForwardingConfig(ctx context.Context, cfgID string) (*LogForwardingConfig, error) {
	var resp LogForwardingConfig
	err := c.Get(ctx, APIMC, fmt.Sprintf("/log/forwarding/%s", cfgID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) UpdateLogForwardingConfig(ctx context.Context, cfgID string, cfg LogForwardingConfig) error {
	return c.Put(ctx, APIMC, fmt.Sprintf("/log/forwarding/%s", cfgID), cfg, nil)
}

func (c *Client) DeleteLogForwarding(ctx context.Context, cfgID string) error {
	return c.Delete(ctx, APIMC, fmt.Sprintf("/log/forwarding/%s", cfgID), nil)
}
