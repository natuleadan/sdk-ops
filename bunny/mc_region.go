package bunny

import "context"

func (c *Client) ListRegions(ctx context.Context) (*ListRegionsResponse, error) {
	var resp ListRegionsResponse
	err := c.Get(ctx, APIMC, "/regions", &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetOptimalBaseRegion(ctx context.Context) (*OptimalBaseRegionResponse, error) {
	var resp OptimalBaseRegionResponse
	err := c.Get(ctx, APIMC, "/regions/optimal", &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetAppRegionSettings(ctx context.Context, appID string) (*RegionSettings, error) {
	var resp RegionSettings
	err := c.Get(ctx, APIMC, "/apps/"+appID+"/region-settings", &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) UpdateAppRegionSettings(ctx context.Context, appID string, req UpdateRegionSettingsRequest) error {
	return c.Put(ctx, APIMC, "/apps/"+appID+"/region-settings", req, nil)
}

func (c *Client) ListNodes(ctx context.Context) ([]string, error) {
	var resp struct {
		Items []string `json:"items"`
	}
	err := c.Get(ctx, APIMC, "/nodes", &resp)
	if err != nil {
		return nil, err
	}
	return resp.Items, nil
}

func (c *Client) ListNodesPlain(ctx context.Context) ([]string, error) {
	var resp []string
	err := c.Get(ctx, APIMC, "/nodes/plain", &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}
