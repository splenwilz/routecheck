package discovery

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ProbeSource discovers endpoints by probing common paths.
// This is a fallback when no OpenAPI spec or manual map is available.
type ProbeSource struct {
	baseURL string
	client  *http.Client
}

// NewProbeSource creates a new probe discovery source.
func NewProbeSource(baseURL string) *ProbeSource {
	return &ProbeSource{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Name returns the source name.
func (s *ProbeSource) Name() string {
	return "auto-probe"
}

// Discover probes common endpoints and returns those that respond.
// This is a best-effort fallback mechanism.
func (s *ProbeSource) Discover(ctx context.Context) ([]RawEndpoint, error) {
	var endpoints []RawEndpoint

	// Common paths to probe
	commonPaths := []string{
		"/",
		"/api",
		"/api/v1",
		"/api/v2",
		"/health",
		"/healthz",
		"/status",
		"/version",
		"/info",
		"/ping",
		"/ready",
		"/live",
		"/metrics",
	}

	// First, check if we can find an OpenAPI spec at common locations
	specPaths := []string{
		"/openapi.json",
		"/openapi.yaml",
		"/swagger.json",
		"/swagger.yaml",
		"/api-docs",
		"/v2/api-docs",
		"/v3/api-docs",
	}

	for _, path := range specPaths {
		url := s.baseURL + path
		if s.checkPath(ctx, url) {
			// Found an OpenAPI spec, delegate to OpenAPI source
			openAPISource := NewOpenAPISource(url, s.baseURL)
			discovered, err := openAPISource.Discover(ctx)
			if err == nil && len(discovered) > 0 {
				return discovered, nil
			}
		}
	}

	// Probe common paths
	for _, path := range commonPaths {
		url := s.baseURL + path
		if s.checkPath(ctx, url) {
			endpoints = append(endpoints, RawEndpoint{
				Path:   path,
				Method: "GET",
				Responses: map[string]string{
					"200": "Discovered via probe",
				},
			})
		}
	}

	if len(endpoints) == 0 {
		return nil, fmt.Errorf("no endpoints discovered via probing")
	}

	return endpoints, nil
}

// checkPath makes a HEAD or GET request to see if a path exists.
func (s *ProbeSource) checkPath(ctx context.Context, url string) bool {
	// Try HEAD first (cheaper)
	req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
	if err != nil {
		return false
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	// Consider 2xx and 3xx as "exists"
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return true
	}

	// If HEAD returns 405, try GET
	if resp.StatusCode == http.StatusMethodNotAllowed {
		req, err = http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return false
		}

		resp, err = s.client.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)

		return resp.StatusCode >= 200 && resp.StatusCode < 400
	}

	return false
}
