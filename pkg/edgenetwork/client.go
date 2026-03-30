package edgenetwork

import (
	"net/http"
	"strings"
)

// Client is a lightweight Gcore API client for Edge Network products (CDN, DNS, FastEdge, WAAP).
type Client struct {
	RootURL string
	APIKey  string
	HTTP    *http.Client
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey == "" {
		return
	}
	if strings.HasPrefix(c.APIKey, "APIKey ") || strings.HasPrefix(c.APIKey, "Bearer ") {
		req.Header.Set("Authorization", c.APIKey)
		return
	}
	req.Header.Set("Authorization", "APIKey "+c.APIKey)
}

