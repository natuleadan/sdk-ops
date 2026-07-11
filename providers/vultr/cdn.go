package vultr

import (
	"context"
	"fmt"

	"github.com/vultr/govultr/v3"
)

type CDNPullZone struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	OriginDomain string `json:"origin_domain"`
	Status      string `json:"status"`
	CDNURL      string `json:"cdn_url"`
}

func (c *Client) ListCDNPullZones(ctx context.Context) ([]CDNPullZone, error) {
	zones, _, resp, err := c.client.CDN.ListPullZones(ctx)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return nil, fmt.Errorf("vultr cdn list: %w", err)
	}
	var result []CDNPullZone
	for _, z := range zones {
		result = append(result, CDNPullZone{
			ID: z.ID, Label: z.Label, OriginDomain: z.OriginDomain, Status: z.Status, CDNURL: z.CDNURL,
		})
	}
	return result, nil
}

func (c *Client) CreateCDNPullZone(ctx context.Context, label, originDomain string) (*CDNPullZone, error) {
	zone, resp, err := c.client.CDN.CreatePullZone(ctx, &govultr.CDNZoneReq{
		Label: label, OriginDomain: originDomain,
	})
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return nil, fmt.Errorf("vultr cdn create: %w", err)
	}
	return &CDNPullZone{ID: zone.ID, Label: zone.Label, OriginDomain: zone.OriginDomain, Status: zone.Status, CDNURL: zone.CDNURL}, nil
}

func (c *Client) DeleteCDNPullZone(ctx context.Context, id string) error {
	return c.client.CDN.DeletePullZone(ctx, id)
}

func (c *Client) PurgeCDNPullZone(ctx context.Context, id string) error {
	return c.client.CDN.PurgePullZone(ctx, id)
}
