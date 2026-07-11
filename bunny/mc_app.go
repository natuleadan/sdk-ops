package bunny

import (
	"context"
	"strconv"
)

func (c *Client) CreateApp(ctx context.Context, req AddApplicationRequest) (*AddApplicationResponse, error) {
	var resp AddApplicationResponse
	err := c.Post(ctx, APIMC, "/apps", req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListApps(ctx context.Context, cursor string, limit int32) (*ListApplicationsResponse, error) {
	path := "/apps"
	q := ""
	if cursor != "" {
		q = "?nextCursor=" + cursor
	}
	if limit > 0 {
		if q == "" {
			q = "?limit="
		} else {
			q += "&limit="
		}
		q += strconv.Itoa(int(limit))
	}
	if q != "" {
		path += q
	}

	var resp ListApplicationsResponse
	err := c.Get(ctx, APIMC, path, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetApp(ctx context.Context, appID string) (*Application, error) {
	var resp Application
	err := c.Get(ctx, APIMC, "/apps/"+appID, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) UpdateApp(ctx context.Context, appID string, req AddApplicationRequest) (*AddApplicationResponse, error) {
	var resp AddApplicationResponse
	err := c.Put(ctx, APIMC, "/apps/"+appID, req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) PatchApp(ctx context.Context, appID string, req PatchApplicationRequest) (*AddApplicationResponse, error) {
	var resp AddApplicationResponse
	err := c.Patch(ctx, APIMC, "/apps/"+appID, req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) DeleteApp(ctx context.Context, appID string) error {
	return c.Delete(ctx, APIMC, "/apps/"+appID, nil)
}

func (c *Client) DeployApp(ctx context.Context, appID string) error {
	return c.Post(ctx, APIMC, "/apps/"+appID+"/deploy", nil, nil)
}

func (c *Client) UndeployApp(ctx context.Context, appID string) error {
	return c.Post(ctx, APIMC, "/apps/"+appID+"/undeploy", nil, nil)
}

func (c *Client) RestartApp(ctx context.Context, appID string) error {
	return c.Post(ctx, APIMC, "/apps/"+appID+"/restart", nil, nil)
}

func (c *Client) GetAppOverview(ctx context.Context, appID string) (*Overview, error) {
	var resp Overview
	err := c.Get(ctx, APIMC, "/apps/"+appID+"/overview", &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetAppStatistics(ctx context.Context, appID, from, to string, granularity DataGranularity) (*Statistics, error) {
	path := "/apps/" + appID + "/statistics?fromDate=" + from + "&granularity=" + string(granularity)
	if to != "" {
		path += "&toDate=" + to
	}
	var resp Statistics
	err := c.Get(ctx, APIMC, path, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetAppUsageSummary(ctx context.Context, appID string) (*UsageSummary, error) {
	var resp UsageSummary
	err := c.Get(ctx, APIMC, "/apps/"+appID+"/summary", &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetUserLimits(ctx context.Context) (*UserLimits, error) {
	var resp UserLimits
	err := c.Get(ctx, APIMC, "/limits", &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetAppAutoscaling(ctx context.Context, appID string) (*AutoscalingSettings, error) {
	var resp AutoscalingSettings
	err := c.Get(ctx, APIMC, "/apps/"+appID+"/autoscaling", &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) UpdateAppAutoscaling(ctx context.Context, appID string, as AutoscalingSettings) error {
	return c.Put(ctx, APIMC, "/apps/"+appID+"/autoscaling", as, nil)
}
