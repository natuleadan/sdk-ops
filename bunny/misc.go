package bunny

import (
	"context"
	"fmt"
)

type Country struct {
	Name    string `json:"Name"`
	IsoCode string `json:"IsoCode"`
	IsEU    bool   `json:"IsEU"`
	TaxRate float64 `json:"TaxRate"`
}

type CDNRegion struct {
	ID              int     `json:"Id"`
	Name            string  `json:"Name"`
	PricePerGigabyte float64 `json:"PricePerGigabyte"`
	RegionCode      string  `json:"RegionCode"`
	ContinentCode   string  `json:"ContinentCode"`
	CountryCode     string  `json:"CountryCode"`
}

type StorageRegion struct {
	ID   int64  `json:"Id"`
	Name string `json:"Name"`
	URL  string `json:"Url"`
}

type UserAuditLog struct {
	ID          int64  `json:"id"`
	Action      string `json:"action"`
	Description string `json:"description"`
	IP          string `json:"ip"`
	Date        string `json:"date"`
}

type UserAuditResponse struct {
	Entries          []UserAuditLog `json:"entries"`
	HasMoreData      bool           `json:"HasMoreData"`
	ContinuationToken string        `json:"ContinuationToken"`
}

type SearchResult struct {
	Type        string `json:"type"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type SearchResults struct {
	Query         string         `json:"Query"`
	Total         int32          `json:"Total"`
	SearchResults []SearchResult `json:"SearchResults"`
}

func (c *Client) GetCountryList(ctx context.Context) ([]Country, error) {
	var resp []Country
	err := c.Get(ctx, APICore, "/country", &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) GetRegionList(ctx context.Context) ([]CDNRegion, error) {
	var resp []CDNRegion
	err := c.Get(ctx, APICore, "/region", &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) GetStorageRegions(ctx context.Context) ([]StorageRegion, error) {
	var resp []StorageRegion
	err := c.Get(ctx, APICore, "/storagezone/regions", &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) GetUserAuditLog(ctx context.Context, date string) (*UserAuditResponse, error) {
	var resp UserAuditResponse
	err := c.Get(ctx, APICore, fmt.Sprintf("/user/audit/%s", date), &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GlobalSearch(ctx context.Context, query string, from, size int) (*SearchResults, error) {
	var resp SearchResults
	path := fmt.Sprintf("/search?query=%s&from=%d&size=%d", query, from, size)
	err := c.Get(ctx, APICore, path, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}
