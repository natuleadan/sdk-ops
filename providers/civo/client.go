package civo

import (
	"fmt"

	"github.com/civo/civogo"
)

type Client struct {
	client *civogo.Client
	apiKey string
}

func New(apiKey, region string) (*Client, error) {
	c, err := civogo.NewClient(apiKey, region)
	if err != nil {
		return nil, fmt.Errorf("civo: new client: %w", err)
	}
	return &Client{client: c, apiKey: apiKey}, nil
}

func (c *Client) withRegion(region string) (*civogo.Client, error) {
	return civogo.NewClient(c.apiKey, region)
}

func regionAlias(r string) string {
	switch r {
	case "lon", "london":
		return "LON1"
	case "nyc", "newyork":
		return "NYC1"
	case "fra", "frankfurt":
		return "FRA1"
	default:
		return r
	}
}
