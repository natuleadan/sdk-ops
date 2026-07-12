package bunny

import (
	"context"
	"fmt"
)

type CDNStatistics struct {
	TotalBandwidthUsed          int64              `json:"TotalBandwidthUsed"`
	TotalOriginTraffic          int64              `json:"TotalOriginTraffic"`
	TotalRequestsServed         int64              `json:"TotalRequestsServed"`
	CacheHitRate                float64            `json:"CacheHitRate"`
	AverageOriginResponseTime   float64            `json:"AverageOriginResponseTime"`
	GeoTrafficDistribution      any                `json:"GeoTrafficDistribution,omitempty"`
	BandwidthUsedChart          map[string]float64 `json:"BandwidthUsedChart,omitempty"`
	BandwidthCachedChart        map[string]float64 `json:"BandwidthCachedChart,omitempty"`
	CacheHitRateChart           map[string]float64 `json:"CacheHitRateChart,omitempty"`
	RequestsServedChart         map[string]float64 `json:"RequestsServedChart,omitempty"`
}

type OriginShieldQueueStats struct {
	CurrentQueueSize   int64 `json:"CurrentQueueSize,omitempty"`
	TotalRequests      int64 `json:"TotalRequests,omitempty"`
	AverageWaitTime    int64 `json:"AverageWaitTime,omitempty"`
}

type SafeHopStats struct {
	TotalFailovers     int64  `json:"TotalFailovers,omitempty"`
	HealthyOrigins     int    `json:"HealthyOrigins,omitempty"`
	UnhealthyOrigins   int    `json:"UnhealthyOrigins,omitempty"`
}

type OptimizerStats struct {
	TotalSavedBytes    int64   `json:"TotalSavedBytes,omitempty"`
	TotalImagesOptimized int64 `json:"TotalImagesOptimized,omitempty"`
}

func (c *Client) GetStatistics(ctx context.Context) (*CDNStatistics, error) {
	var resp CDNStatistics
	err := c.Get(ctx, APICore, "/statistics", &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetOriginShieldQueueStatistics(ctx context.Context, pullZoneID int64) (*OriginShieldQueueStats, error) {
	var resp OriginShieldQueueStats
	err := c.Get(ctx, APICore, fmt.Sprintf("/pullzone/%d/originshield/queuestatistics", pullZoneID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetSafeHopStatistics(ctx context.Context, pullZoneID int64) (*SafeHopStats, error) {
	var resp SafeHopStats
	err := c.Get(ctx, APICore, fmt.Sprintf("/pullzone/%d/safehop/statistics", pullZoneID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetOptimizerStatistics(ctx context.Context, pullZoneID int64) (*OptimizerStats, error) {
	var resp OptimizerStats
	err := c.Get(ctx, APICore, fmt.Sprintf("/pullzone/%d/optimizer/statistics", pullZoneID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetDNSZoneStatistics(ctx context.Context, zoneID int64) (*map[string]any, error) {
	var resp map[string]any
	err := c.Get(ctx, APICore, fmt.Sprintf("/dnszone/%d/statistics", zoneID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetStorageZoneStatistics(ctx context.Context, zoneID int64) (*map[string]any, error) {
	var resp map[string]any
	err := c.Get(ctx, APICore, fmt.Sprintf("/storagezone/%d/statistics", zoneID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetStorageZoneEgressStatistics(ctx context.Context, zoneID int64) (*map[string]any, error) {
	var resp map[string]any
	err := c.Get(ctx, APICore, fmt.Sprintf("/storagezone/%d/statistics/egress", zoneID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetVideoLibraryDRMStatistics(ctx context.Context, libID int64) (*map[string]any, error) {
	var resp map[string]any
	err := c.Get(ctx, APICore, fmt.Sprintf("/videolibrary/%d/drm/statistics", libID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetVideoLibraryTranscribingStatistics(ctx context.Context, libID int64) (*map[string]any, error) {
	var resp map[string]any
	err := c.Get(ctx, APICore, fmt.Sprintf("/videolibrary/%d/transcribing/statistics", libID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}
