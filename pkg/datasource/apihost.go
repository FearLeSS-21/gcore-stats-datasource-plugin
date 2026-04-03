package datasource

import (
	"fmt"
	"net/url"
	"strings"
)

// ValidateAPIBaseURL checks jsonData apiUrl before instantiating the datasource.
// Empty or whitespace-only input is valid (caller substitutes default).
// Otherwise: parseable as http(s) URL, host must be a Gcore API host (prefix "api." and suffix ".gcore.com"),
// no path/query/fragment/userinfo. This prevents proxy/backend traffic to arbitrary api.*.com hosts.
func ValidateAPIBaseURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	u := raw
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = "https://" + u
	}
	u = strings.TrimSuffix(u, "/")

	parsed, err := url.Parse(u)
	if err != nil {
		return fmt.Errorf("invalid apiUrl: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("invalid apiUrl: only http and https are allowed")
	}
	if parsed.User != nil {
		return fmt.Errorf("invalid apiUrl: userinfo is not allowed")
	}
	host := strings.ToLower(parsed.Hostname())
	const gcoreAPISuffix = ".gcore.com"
	if !strings.HasPrefix(host, "api.") || !strings.HasSuffix(host, gcoreAPISuffix) {
		return fmt.Errorf("invalid apiUrl: host must be a Gcore API hostname (api.*.gcore.com, e.g. api.gcore.com)")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return fmt.Errorf("invalid apiUrl: path is not allowed")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("invalid apiUrl: query or fragment is not allowed")
	}
	return nil
}
