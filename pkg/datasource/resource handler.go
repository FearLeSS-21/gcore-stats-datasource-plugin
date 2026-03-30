package datasource

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

func (ds *GCDataSource) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	path := strings.TrimPrefix(strings.TrimSpace(req.Path), "/")
	if path == "" {
		path = strings.TrimSpace(req.URL)
		path = strings.TrimPrefix(path, "/")
	}
	if path == "" {
		sender.Send(&backend.CallResourceResponse{Status: 400, Body: []byte(`{"error":"missing path"}`)})
		return nil
	}
	rootURL := ds.rootURL()
	var upstreamPath string
	switch path {
	case "iam/users/me", "users/me":
		upstreamPath = path
	case "cdn/resources":
		upstreamPath = "cdn/resources"
	case "dns/zones":
		upstreamPath = "dns/v2/zones"
	case "fastedge/apps":
		upstreamPath = "fastedge/v1/apps"
	default:
		sender.Send(&backend.CallResourceResponse{Status: 404, Body: []byte(`{"error":"unknown path"}`)})
		return nil
	}
	url := rootURL + "/" + upstreamPath
	if req.URL != "" {
		if idx := strings.Index(req.URL, "?"); idx >= 0 {
			url = rootURL + "/" + upstreamPath + req.URL[idx:]
		}
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, url, nil)
	if err != nil {
		sender.Send(&backend.CallResourceResponse{Status: 500, Body: []byte(fmt.Sprintf(`{"error":"%s"}`, err.Error()))})
		return nil
	}
	ds.setHeaders(httpReq)
	resp, err := ds.Client.Do(httpReq)
	if err != nil {
		sender.Send(&backend.CallResourceResponse{Status: 502, Body: []byte(fmt.Sprintf(`{"error":"request failed: %s"}`, err.Error()))})
		return nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		sender.Send(&backend.CallResourceResponse{Status: 502, Body: []byte(fmt.Sprintf(`{"error":"read body: %s"}`, err.Error()))})
		return nil
	}
	status := resp.StatusCode
	if status >= 500 {
		status = 502
		body = []byte(`{"error":"Gcore API server error. Please try again later."}`)
	}
	sender.Send(&backend.CallResourceResponse{
		Status:  status,
		Body:    body,
		Headers: map[string][]string{"Content-Type": {"application/json"}},
	})
	return nil
}
