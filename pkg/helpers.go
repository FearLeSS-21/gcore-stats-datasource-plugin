package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

func parseUnifiedQuery(query backend.DataQuery) (*QueryModel, error) {
	if len(query.JSON) == 0 {
		return nil, fmt.Errorf("query JSON is empty")
	}
	var qm QueryModel
	if err := json.Unmarshal(query.JSON, &qm); err != nil {
		return nil, err
	}
	return &qm, nil
}

func buildStatsRequest(query backend.DataQuery) (*StatsRequest, error) {
	qm, err := parseUnifiedQuery(query)
	if err != nil {
		return nil, err
	}
	if qm.Metric.Value == "" {
		return nil, fmt.Errorf("no metric selected")
	}

	metric := strings.TrimSpace(qm.Metric.Value)

	granularity := qm.Granularity.Value
	if granularity == "" {
		granularity = "1h"
	}

	switch metric {
	case "bandwidth":
		metric = "max_bandwidth"
		granularity = ""
	}

	return &StatsRequest{
		Metrics:     []string{metric},
		Granularity: granularity,
		From:        query.TimeRange.From.UTC().Format(time.RFC3339),
		To:          query.TimeRange.To.UTC().Format(time.RFC3339),
		Vhosts:      parseStrings(qm.Vhosts),
		Resources:   parseInts(qm.Resources),
		Countries:   parseStrings(qm.Countries),
		Regions:     parseStrings(qm.Regions),
		Clients:     parseInts(qm.Clients),
		GroupBy:     extractGrouping(qm.Grouping),
		Flat:        true,
	}, nil
}

func extractGrouping(values []SelectableValue) []string {
	var out []string
	for _, v := range values {
		if v.Value != "" {
			out = append(out, v.Value)
		}
	}
	return out
}

func parseInts(s string) []int64 {
	var out []int64
	for _, p := range strings.Split(s, ",") {
		if v, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64); err == nil {
			out = append(out, v)
		}
	}
	return out
}

func parseStrings(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// DnsGranularityToAPI returns the value to send to Gcore DNS API (e.g. "5m", "1h").
func DnsGranularityToAPI(g string) string {
	g = strings.TrimSpace(strings.ToLower(g))
	if g == "" {
		return "5m"
	}
	switch g {
	case "5m", "10m", "15m", "30m", "1h", "1.5h", "2h45m", "24h":
		return g
	}
	if _, err := strconv.Atoi(g); err == nil {
		return g + "s"
	}
	return "5m"
}
