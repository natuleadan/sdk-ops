package vultr

import (
	"net/http"

	"github.com/vultr/govultr/v3"
)

type Client struct {
	client     *govultr.Client
	token      string
	baseURL    string
	httpClient *http.Client
}

type tokenTransport struct {
	token     string
	transport http.RoundTripper
}

func (t *tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.transport.RoundTrip(req)
}

func New(token string) *Client {
	httpClient := &http.Client{
		Transport: &tokenTransport{
			token:     token,
			transport: http.DefaultTransport,
		},
	}
	return &Client{
		client:     govultr.NewClient(httpClient),
		token:      token,
		baseURL:    "https://api.vultr.com",
		httpClient: httpClient,
	}
}
