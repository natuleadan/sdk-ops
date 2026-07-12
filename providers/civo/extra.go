package civo

import (
	"context"
	"fmt"

	"github.com/natuleadan/sdk-ops/providers"
)

func (c *Client) CreateBareMetal(ctx context.Context, cfg providers.BareMetalCreateConfig) (*providers.BareMetal, error) {
	return nil, fmt.Errorf("civo: method not available")
}

func (c *Client) DeleteBareMetal(ctx context.Context, id string) error {
	return fmt.Errorf("civo: method not available")
}

func (c *Client) ListBareMetal(ctx context.Context) ([]providers.BareMetal, error) {
	return nil, fmt.Errorf("civo: method not available")
}
