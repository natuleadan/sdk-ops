package digitalocean

import (
	"github.com/digitalocean/godo"
)

type Client struct {
	client *godo.Client
}

func New(token string) *Client {
	return &Client{
		client: godo.NewFromToken(token),
	}
}
