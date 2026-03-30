package datasource

import (
	"context"

	"github.com/FearLeSS-21/cdn-stats-datasource-plugin/core"
	"github.com/FearLeSS-21/cdn-stats-datasource-plugin/edgenetwork"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

func (ds *GCDataSource) queryWAAP(ctx context.Context, query backend.DataQuery, qm *core.QueryModel) backend.DataResponse {
	client := &edgenetwork.Client{
		RootURL: ds.rootURL(),
		APIKey:  ds.APIKey,
		HTTP:    ds.Client,
	}
	frames, err := client.QueryWAAP(ctx, qm, query.TimeRange)
	if err != nil {
		return backend.DataResponse{Error: err}
	}
	return backend.DataResponse{Frames: frames}
}
