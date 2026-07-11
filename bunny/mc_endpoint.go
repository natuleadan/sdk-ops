package bunny

import (
	"context"
	"fmt"
)

func (c *Client) AddEndpoint(ctx context.Context, appID, containerID string, req EndpointRequest) (*SaveEndpointResponse, error) {
	var resp SaveEndpointResponse
	err := c.Post(ctx, APIMC, fmt.Sprintf("/apps/%s/containers/%s/endpoints", appID, containerID), req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListEndpoints(ctx context.Context, appID string) (*ListEndpointsResponse, error) {
	var resp ListEndpointsResponse
	err := c.Get(ctx, APIMC, fmt.Sprintf("/apps/%s/endpoints", appID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) UpdateEndpoint(ctx context.Context, appID, endpointID string, req EndpointRequest) error {
	return c.Put(ctx, APIMC, fmt.Sprintf("/apps/%s/endpoints/%s", appID, endpointID), req, nil)
}

func (c *Client) DeleteEndpoint(ctx context.Context, appID, endpointID string) error {
	return c.Delete(ctx, APIMC, fmt.Sprintf("/apps/%s/endpoints/%s", appID, endpointID), nil)
}
