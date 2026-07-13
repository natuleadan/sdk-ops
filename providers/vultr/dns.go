package vultr

import (
	"context"
	"fmt"
	"log"

	"github.com/vultr/govultr/v3"

	"github.com/natuleadan/sdk-ops/providers"
)

func (c *Client) ListDNSZones(ctx context.Context) ([]providers.DNSZone, error) {
	domains, _, resp, err := c.client.Domain.List(ctx, &govultr.ListOptions{})
	if resp != nil {
		defer func() { if err := resp.Body.Close(); err != nil { log.Printf("vultr: body close error: %v", err) } }()
	}
	if err != nil {
		return nil, fmt.Errorf("vultr list domains: %w", err)
	}
	var result []providers.DNSZone
	for _, d := range domains {
		result = append(result, providers.DNSZone{
			ID:   d.Domain,
			Name: d.Domain,
		})
	}
	return result, nil
}

func (c *Client) CreateDNSRecord(ctx context.Context, zoneID string, r providers.DNSRecord) error {
	req := &govultr.DomainRecordCreateReq{
		Type: r.Type,
		Name: r.Name,
		Data: r.Value,
		TTL:  r.TTL,
	}
	_, resp, err := c.client.DomainRecord.Create(ctx, zoneID, req)
	if resp != nil {
		defer func() { if err := resp.Body.Close(); err != nil { log.Printf("vultr: body close error: %v", err) } }()
	}
	return err
}

func (c *Client) DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error {
	err := c.client.DomainRecord.Delete(ctx, zoneID, recordID)
	return err
}
