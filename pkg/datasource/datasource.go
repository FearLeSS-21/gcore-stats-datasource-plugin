package datasource

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/FearLeSS-21/cdn-stats-datasource-plugin/core"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

type GCDataSource struct {
	URL    string
	APIKey string
	Client *http.Client
}

func NewDataSource(url, apiKey string) *GCDataSource {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}
	url = strings.TrimSuffix(url, "/")

	return &GCDataSource{
		URL:    url,
		APIKey: apiKey,
		Client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (ds *GCDataSource) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if ds.APIKey != "" {
		if strings.HasPrefix(ds.APIKey, "APIKey ") || strings.HasPrefix(ds.APIKey, "Bearer ") {
			req.Header.Set("Authorization", ds.APIKey)
		} else {
			req.Header.Set("Authorization", "APIKey "+ds.APIKey)
		}
	}
}

func (ds *GCDataSource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	resp := backend.NewQueryDataResponse()
	for _, q := range req.Queries {
		resp.Responses[q.RefID] = ds.query(ctx, q)
	}
	return resp, nil
}

func (ds *GCDataSource) rootURL() string {
	u := strings.TrimSuffix(ds.URL, "/")
	for _, suffix := range []string{"/cdn", "/dns", "/fastedge", "/waap"} {
		if strings.HasSuffix(u, suffix) {
			return strings.TrimSuffix(u, suffix)
		}
	}
	return u
}

func (ds *GCDataSource) query(ctx context.Context, query backend.DataQuery) backend.DataResponse {
	qm, err := core.ParseUnifiedQuery(query)
	if err != nil {
		return backend.DataResponse{Error: err}
	}
	product := strings.TrimSpace(strings.ToLower(qm.Product))
	switch product {
	case "dns":
		return ds.queryDNS(ctx, query, qm)
	case "fastedge":
		return ds.queryFastEdge(ctx, query, qm)
	case "waap":
		return ds.queryWAAP(ctx, query, qm)
	default:
		return ds.queryCDN(ctx, query)
	}
}
