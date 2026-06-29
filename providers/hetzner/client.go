package hetzner

import (
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

type Client struct {
	client *hcloud.Client
	token  string
}

func New(token string) *Client {
	return &Client{
		client: hcloud.NewClient(hcloud.WithToken(token)),
		token:  token,
	}
}
