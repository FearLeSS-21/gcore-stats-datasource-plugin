package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

type GCDataSource struct {
	URL    string
	APIKey string
	Client *http.Client
}

type IAMUser struct {
	Name string `json:"name"`
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
	// Route unified query model to one of the product-specific handlers (CDN, DNS, FastEdge, WAAP).
	qm, err := parseUnifiedQuery(query)
	if err != nil {
		return backend.DataResponse{Error: err}
	}
	product := strings.TrimSpace(strings.ToLower(qm.Product))
	switch product {
	case "dns":
		// DNS Edge
		return ds.queryDNS(ctx, query, qm)
	case "fastedge":
		// FastEdge Edge
		return ds.queryFastEdge(ctx, query, qm)
	case "waap":
		// WAAP Edge
		return ds.queryWAAP(ctx, query, qm)
	default:
		// CDN Edge
		return ds.queryCDN(ctx, query)
	}
}

// CDN Edge
// CDN query handler: converts unified query model into CDN StatsRequest and maps the response into Grafana frames.
func (ds *GCDataSource) queryCDN(ctx context.Context, query backend.DataQuery) backend.DataResponse {
	payload, err := buildStatsRequest(query)
	if err != nil {
		return backend.DataResponse{Error: err}
	}
	stats, err := ds.fetchStats(ctx, payload)
	if err != nil {
		return backend.DataResponse{Error: err}
	}
	return backend.DataResponse{Frames: transformStatsToFrames(stats, payload)}
}

// CDN: call Gcore CDN statistics aggregate API with the prepared payload.
func (ds *GCDataSource) fetchStats(ctx context.Context, payload *StatsRequest) ([]StatsResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	baseURL := ds.rootURL() + "/cdn"

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		baseURL+"/statistics/aggregate/stats",
		bytes.NewBuffer(body),
	)
	if err != nil {
		return nil, err
	}

	ds.setHeaders(req)

	resp, err := ds.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 204 No Content: API returns this when there is no data (e.g. for bandwidth, WAF requests in some time ranges).
	// Treat as success with empty result so the panel shows no series instead of an error.
	if resp.StatusCode == http.StatusNoContent {
		return []StatsResponse{}, nil
	}

	if err := handleAPIError(resp.StatusCode, raw); err != nil {
		return nil, err
	}

	var out []StatsResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}

	return out, nil
}

func handleAPIError(statusCode int, body []byte) error {
	if statusCode == http.StatusOK || statusCode == http.StatusNoContent {
		return nil
	}

	errorMsg := strings.TrimSpace(string(body))

	switch statusCode {
	case http.StatusBadRequest:
		return fmt.Errorf("bad request (400): %s. Please check your query parameters", errorMsg)
	case http.StatusUnauthorized:
		
		return fmt.Errorf("datasource authentication error: invalid API key. Please verify your credentials in the datasource settings")
	case http.StatusForbidden:
		return fmt.Errorf("forbidden (403): access denied. You may not have permissions to access this resource")
	case http.StatusNotFound:
		return fmt.Errorf("not found (404): resource not found. Check if the URL/CDN resource exists")
	case http.StatusTooManyRequests:
		return fmt.Errorf("too many requests (429): rate limit exceeded. Please reduce query frequency")
	}

	if statusCode >= 500 {
		return fmt.Errorf("server error (%d): %s. This is an issue with the Gcore API", statusCode, errorMsg)
	}

	return fmt.Errorf("gcore api error (%d): %s", statusCode, errorMsg)
}

// DNS Edge
// DNS query handler: resolves zones based on query model and aggregates per-zone statistics.
func (ds *GCDataSource) queryDNS(ctx context.Context, query backend.DataQuery, qm *QueryModel) backend.DataResponse {
	zones, err := ds.dnsResolveZones(ctx, qm)
	if err != nil {
		return backend.DataResponse{Error: err}
	}
	if len(zones) == 0 {
		return backend.DataResponse{Frames: ds.dnsTransformToFrames(nil, qm)}
	}
	aggregated := make(map[string]float64)
	for _, zoneName := range zones {
		stats, err := ds.dnsFetchZoneStats(ctx, zoneName, qm, query.TimeRange)
		if err != nil || stats == nil {
			continue
		}
		for ts, val := range stats.Requests {
			aggregated[ts] += val
		}
	}
	return backend.DataResponse{Frames: ds.dnsTransformToFrames(&DNSStatsResponse{Requests: aggregated}, qm)}
}

// DNS: determine which zones should be queried based on the unified query model.
func (ds *GCDataSource) dnsResolveZones(ctx context.Context, qm *QueryModel) ([]string, error) {
	if qm.Zone != "" && qm.Zone != "all" {
		if strings.Contains(qm.Zone, ",") {
			return parseStrings(qm.Zone), nil
		}
		return []string{qm.Zone}, nil
	}
	allZones, err := ds.dnsGetAllZones(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(allZones))
	for _, z := range allZones {
		out = append(out, z.Name)
	}
	return out, nil
}

// DNS: fetch all zones from the Gcore DNS API, handling pagination.
func (ds *GCDataSource) dnsGetAllZones(ctx context.Context) ([]Zone, error) {
	baseURL := ds.rootURL() + "/dns"
	var allZones []Zone
	limit := 1000
	offset := 0
	for {
		url := fmt.Sprintf("%s/v2/zones?limit=%d&offset=%d", baseURL, limit, offset)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		ds.setHeaders(req)
		resp, err := ds.Client.Do(req)
		if err != nil {
			return nil, err
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err := handleAPIError(resp.StatusCode, raw); err != nil {
			return nil, err
		}
		var zonesResp ZonesResponse
		if err := json.Unmarshal(raw, &zonesResp); err != nil {
			return nil, err
		}
		allZones = append(allZones, zonesResp.Zones...)
		if len(zonesResp.Zones) < limit || (zonesResp.TotalAmount > 0 && offset+limit >= zonesResp.TotalAmount) {
			break
		}
		offset += limit
	}
	return allZones, nil
}

// DNS: fetch statistics for a single zone and record type over the requested time range.
func (ds *GCDataSource) dnsFetchZoneStats(ctx context.Context, zoneName string, qm *QueryModel, tr backend.TimeRange) (*DNSStatsResponse, error) {
	baseURL := ds.rootURL() + "/dns"
	url := fmt.Sprintf("%s/v2/zones/%s/statistics", baseURL, zoneName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	q.Add("from", fmt.Sprintf("%d", tr.From.Unix()))
	q.Add("to", fmt.Sprintf("%d", tr.To.Unix()))
	gran := qm.DnsGranularity.Value
	if gran == "" {
		gran = "5m"
	}
	q.Add("granularity", DnsGranularityToAPI(gran))
	if qm.RecordType != "" && qm.RecordType != "ALL" {
		q.Add("record_type", qm.RecordType)
	}
	req.URL.RawQuery = q.Encode()
	ds.setHeaders(req)
	resp, err := ds.Client.Do(req)
	if err != nil {
		return nil, err
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, nil
	}
	if err := handleAPIError(resp.StatusCode, raw); err != nil {
		return nil, err
	}
	var stats DNSStatsResponse
	if err := json.Unmarshal(raw, &stats); err != nil {
		return nil, err
	}
	return &stats, nil
}

// DNS: transform aggregated DNS statistics into a single time series frame for Grafana.
func (ds *GCDataSource) dnsTransformToFrames(stats *DNSStatsResponse, qm *QueryModel) []*data.Frame {
	frame := data.NewFrame("Statistics",
		data.NewField("time", nil, []time.Time{}),
		data.NewField("value", nil, []float64{}),
	)
	displayName := "Requests"
	if qm != nil && qm.DnsLegendFormat != "" {
		displayName = qm.DnsLegendFormat
	} else if qm != nil {
		if qm.Zone != "" && qm.Zone != "all" {
			displayName = "Requests (" + qm.Zone + ")"
		} else {
			displayName = "Requests (All Zones)"
		}
		if qm.RecordType != "" && qm.RecordType != "ALL" {
			displayName += ", " + qm.RecordType
		}
	}
	frame.Fields[1].SetConfig(&data.FieldConfig{DisplayName: displayName, Unit: "short"})
	if stats == nil || len(stats.Requests) == 0 {
		return []*data.Frame{frame}
	}
	keys := make([]string, 0, len(stats.Requests))
	for k := range stats.Requests {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		ts, err := strconv.ParseInt(k, 10, 64)
		if err != nil {
			continue
		}
		if ts > 10000000000 {
			ts = ts / 1000
		}
		frame.AppendRow(time.Unix(ts, 0), stats.Requests[k])
	}
	return []*data.Frame{frame}
}

// FastEdge Edge
// FastEdge query handler: call FastEdge app duration API using parameters from the unified query model.
func (ds *GCDataSource) queryFastEdge(ctx context.Context, query backend.DataQuery, qm *QueryModel) backend.DataResponse {
	baseURL := ds.rootURL()
	reqURL := baseURL + "/fastedge/v1/stats/app_duration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return backend.DataResponse{Error: err}
	}
	q := req.URL.Query()
	q.Add("from", query.TimeRange.From.UTC().Format(time.RFC3339))
	q.Add("to", query.TimeRange.To.UTC().Format(time.RFC3339))
	step := qm.Step
	if step <= 0 {
		step = 60
	}
	q.Add("step", fmt.Sprintf("%d", step))
	if qm.AppId != 0 {
		q.Add("id", fmt.Sprintf("%d", qm.AppId))
	}
	if qm.Network != "" {
		q.Add("network", qm.Network)
	}
	req.URL.RawQuery = q.Encode()
	ds.setHeaders(req)
	resp, err := ds.Client.Do(req)
	if err != nil {
		return backend.DataResponse{Error: err}
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err := handleAPIError(resp.StatusCode, raw); err != nil {
		return backend.DataResponse{Error: err}
	}
	var out FastEdgeResponseStats
	if err := json.Unmarshal(raw, &out); err != nil {
		return backend.DataResponse{Error: err}
	}
	return backend.DataResponse{Frames: ds.fastEdgeTransformToFrames(&out, qm)}
}

// FastEdge: convert app duration statistics into a time series frame for the selected metric.
func (ds *GCDataSource) fastEdgeTransformToFrames(response *FastEdgeResponseStats, qm *QueryModel) []*data.Frame {
	if response == nil || len(response.Stats) == 0 {
		return nil
	}
	metric := qm.FastedgeMetric
	if metric == "" {
		metric = "avg"
	}
	n := len(response.Stats)
	times := make([]time.Time, n)
	values := make([]float64, n)
	for i, s := range response.Stats {
		t, _ := time.Parse(time.RFC3339, s.Time)
		times[i] = t
		switch metric {
		case "min":
			values[i] = s.Min
		case "max":
			values[i] = s.Max
		case "median":
			values[i] = s.Median
		case "perc75":
			values[i] = s.Perc75
		case "perc90":
			values[i] = s.Perc90
		default:
			values[i] = s.Avg
		}
	}
	name := metric
	if qm.AppName != "" {
		name = qm.AppName + " " + metric
	}
	frame := data.NewFrame(name,
		data.NewField("Time", nil, times),
		data.NewField("value", nil, values),
	)
	frame.SetMeta(&data.FrameMeta{Type: data.FrameTypeTimeSeriesMulti})
	return []*data.Frame{frame}
}

// WAAP Edge
// WAAP query handler: fetch WAAP statistics for the selected metric and granularity.
func (ds *GCDataSource) queryWAAP(ctx context.Context, query backend.DataQuery, qm *QueryModel) backend.DataResponse {
	baseURL := ds.rootURL()
	url := baseURL + "/waap/v1/statistics/series"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return backend.DataResponse{Error: err}
	}
	granularity := qm.WaapGranularity
	if granularity == "" {
		granularity = "1h"
	}
	metric := qm.WaapMetric
	if metric == "" {
		metric = "total_requests"
	}
	q := req.URL.Query()
	q.Add("from", query.TimeRange.From.UTC().Format(time.RFC3339))
	q.Add("to", query.TimeRange.To.UTC().Format(time.RFC3339))
	q.Add("granularity", granularity)
	q.Add("metrics", metric)
	req.URL.RawQuery = q.Encode()
	ds.setHeaders(req)
	resp, err := ds.Client.Do(req)
	if err != nil {
		return backend.DataResponse{Error: err}
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err := handleAPIError(resp.StatusCode, raw); err != nil {
		return backend.DataResponse{Error: err}
	}
	var rawMap map[string][]map[string]interface{}
	if err := json.Unmarshal(raw, &rawMap); err != nil {
		return backend.DataResponse{Error: err}
	}
	stats := make(map[string][]WaapDataPoint)
	for metricKey, rawPoints := range rawMap {
		var points []WaapDataPoint
		for _, rp := range rawPoints {
			dt := ""
			for _, key := range []string{"date_time", "datetime", "date", "timestamp", "time"} {
				if v, ok := rp[key]; ok {
					dt = fmt.Sprintf("%v", v)
					break
				}
			}
			val := 0.0
			for _, key := range []string{"value", "count", "total", "sum", "requests"} {
				if v, ok := rp[key]; ok {
					switch n := v.(type) {
					case float64:
						val = n
					case int:
						val = float64(n)
					case int64:
						val = float64(n)
					}
					break
				}
			}
			points = append(points, WaapDataPoint{DateTime: dt, Value: val})
		}
		stats[metricKey] = points
	}
	waapResp := &WaapStatsResponse{Data: stats}
	return backend.DataResponse{Frames: ds.waapTransformToFrames(waapResp, qm)}
}

// WAAP: transform WAAP statistics into one or more time series frames, applying legend and units.
func (ds *GCDataSource) waapTransformToFrames(stats *WaapStatsResponse, qm *QueryModel) []*data.Frame {
	var frames []*data.Frame
	if stats == nil || len(stats.Data) == 0 {
		return frames
	}
	for metricName, points := range stats.Data {
		frame := data.NewFrame(metricName,
			data.NewField("time", nil, []time.Time{}),
			data.NewField("value", nil, []float64{}),
		)
		displayName := metricName
		if qm != nil && qm.WaapLegendFormat != "" {
			displayName = qm.WaapLegendFormat
		}
		unit := "short"
		if metricName == "total_bytes" {
			unit = "kbytes"
		}
		frame.Fields[1].SetConfig(&data.FieldConfig{DisplayName: displayName, Unit: unit})
		for _, point := range points {
			t, err := time.Parse(time.RFC3339, point.DateTime)
			if err != nil {
				t, _ = time.Parse("2006-01-02T15:04:05Z07:00", point.DateTime)
			}
			val := point.Value
			if metricName == "total_bytes" {
				val = val / 1024
			}
			frame.AppendRow(t, val)
		}
		frames = append(frames, frame)
	}
	return frames
}

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
		Status: status,
		Body:   body,
		Headers: map[string][]string{"Content-Type": {"application/json"}},
	})
	return nil
}

func (ds *GCDataSource) CheckHealth(ctx context.Context, req *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	rootURL := ds.rootURL()
	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodGet, rootURL+"/iam/users/me", nil)
	if err != nil {
		return &backend.CheckHealthResult{Status: backend.HealthStatusError, Message: err.Error()}, nil
	}

	ds.setHeaders(reqHTTP)

	resp, err := ds.Client.Do(reqHTTP)
	if err != nil {
		return &backend.CheckHealthResult{Status: backend.HealthStatusError, Message: err.Error()}, nil
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return &backend.CheckHealthResult{Status: backend.HealthStatusError, Message: err.Error()}, nil
	}

	if err := handleAPIError(resp.StatusCode, raw); err != nil {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: err.Error(),
		}, nil
	}

	var user IAMUser
	if err := json.Unmarshal(raw, &user); err != nil {
		return &backend.CheckHealthResult{Status: backend.HealthStatusError, Message: err.Error()}, nil
	}

	name := "Unknown"
	if user.Name != "" {
		name = user.Name
	}

	return &backend.CheckHealthResult{
		Status:  backend.HealthStatusOk,
		Message: fmt.Sprintf("Auth OK (IAM): %s", name),
	}, nil
}

func transformStatsToFrames(stats []StatsResponse, req *StatsRequest) []*data.Frame {
	var frames []*data.Frame

	for _, s := range stats {
		var groups []string

		if s.Resource != nil {
			groups = append(groups, fmt.Sprintf("resource: %d", *s.Resource))
		}
		if s.Client != nil {
			groups = append(groups, fmt.Sprintf("client: %d", *s.Client))
		}
		if s.Region != "" {
			groups = append(groups, fmt.Sprintf("region: %s", s.Region))
		}
		if s.Vhost != "" {
			groups = append(groups, fmt.Sprintf("vhost: %s", s.Vhost))
		}
		if s.Country != "" {
			groups = append(groups, fmt.Sprintf("country: %s", s.Country))
		}
		if s.DC != "" {
			groups = append(groups, fmt.Sprintf("dc: %s", s.DC))
		}

		suffix := ""
		if len(groups) > 0 {
			suffix = " (" + strings.Join(groups, ", ") + ")"
		}

		for metric, raw := range s.Metrics {
			// Metrics can be returned either as a time series ([[ts, value], ...])
			// or as a single aggregated number (e.g. max_bandwidth without granularity).
			// Try to decode as a series first; if that fails, fall back to a scalar.
			var points [][2]float64
			if err := json.Unmarshal(raw, &points); err != nil || len(points) == 0 {
				var scalar float64
				if err := json.Unmarshal(raw, &scalar); err != nil {
					// Unsupported shape, skip this metric.
					continue
				}
				// Synthesize a flat line over the requested time range for scalar metrics.
				fromTime := time.Now()
				toTime := fromTime
				if req != nil {
					if t, err := time.Parse(time.RFC3339, req.From); err == nil {
						fromTime = t
					}
					if t, err := time.Parse(time.RFC3339, req.To); err == nil {
						toTime = t
					}
				}
				points = [][2]float64{
					{float64(fromTime.Unix()), scalar},
					{float64(toTime.Unix()), scalar},
				}
			}

			name := formatMetricName(metric) + suffix

			unit := "decbytes"
			lm := strings.ToLower(metric)
			if strings.Contains(lm, "bandwidth") {
				unit = "bps"
			} else if strings.Contains(lm, "requests") || strings.Contains(lm, "responses") {
				unit = "short"
			}

			valueField := data.NewField("value", nil, []float64{})
			valueField.SetConfig(&data.FieldConfig{
				DisplayName: name,
				Unit:        unit,
			})

			frame := data.NewFrame(
				name,
				data.NewField("time", nil, []time.Time{}),
				valueField,
			)

			frame.SetMeta(&data.FrameMeta{Type: "timeseries"})

			for _, p := range points {
				if len(p) == 2 {
					frame.AppendRow(time.Unix(int64(p[0]), 0), p[1])
				}
			}

			frames = append(frames, frame)
		}
	}

	return frames
}

func formatMetricName(input string) string {
	s := strings.ReplaceAll(input, "_", " ")
	words := strings.Fields(s)
	for i, w := range words {
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}
