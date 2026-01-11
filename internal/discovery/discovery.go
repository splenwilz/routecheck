// Package discovery implements endpoint source resolution.
// This is Layer 2 of the pipeline - it finds endpoints from various sources.
// Priority order: OpenAPI → Manual → Auto-probe
package discovery

import (
	"context"
	"fmt"
)

// RawEndpoint represents an endpoint as discovered from a source.
// This is NOT the canonical model - it will be normalized in the next layer.
type RawEndpoint struct {
	Path        string
	Method      string
	OperationID string            // From OpenAPI, may be empty
	Summary     string            // Human description, may be empty
	Parameters  []RawParameter    // All parameters (path, query, header)
	RequestBody *RawRequestBody   // Request body spec, may be nil
	Security    []RawSecurityReq  // Security requirements
	Responses   map[string]string // Status code -> description
}

// RawParameter represents a parameter as discovered from a source.
type RawParameter struct {
	Name     string
	In       string // "path", "query", "header", "cookie"
	Type     string
	Required bool
	Default  any
	Example  any
}

// RawRequestBody represents a request body as discovered from a source.
type RawRequestBody struct {
	Required    bool
	ContentType string
	Schema      map[string]any
	Example     any
}

// RawSecurityReq represents a security requirement from the source.
type RawSecurityReq struct {
	Name   string   // Security scheme name
	Scopes []string // OAuth scopes, if applicable
}

// Source is the interface that all endpoint sources must implement.
type Source interface {
	// Name returns a human-readable name for this source.
	Name() string

	// Discover finds endpoints from this source.
	// Returns an error if the source cannot be read.
	Discover(ctx context.Context) ([]RawEndpoint, error)
}

// Config holds configuration for endpoint discovery.
type Config struct {
	BaseURL     string // Base URL of the API
	OpenAPIPath string // Path or URL to OpenAPI spec (optional)
	ManualPath  string // Path to manual endpoint map (optional)
	AutoProbe   bool   // Enable auto-probing as fallback
}

// Discover finds endpoints using available sources in priority order.
// Explicitly configured sources (OpenAPI, Manual) fail fast on error.
// Auto-probe is a silent fallback.
func Discover(ctx context.Context, cfg Config) ([]RawEndpoint, string, error) {
	// Priority 1: OpenAPI spec (explicit - fail fast on error)
	if cfg.OpenAPIPath != "" {
		src := NewOpenAPISource(cfg.OpenAPIPath, cfg.BaseURL)
		endpoints, err := src.Discover(ctx)
		if err != nil {
			return nil, "", fmt.Errorf("openapi: %w", err)
		}
		if len(endpoints) > 0 {
			return endpoints, src.Name(), nil
		}
	}

	// Priority 2: Manual endpoint map (explicit - fail fast on error)
	if cfg.ManualPath != "" {
		src := NewManualSource(cfg.ManualPath, cfg.BaseURL)
		endpoints, err := src.Discover(ctx)
		if err != nil {
			return nil, "", fmt.Errorf("manual map: %w", err)
		}
		if len(endpoints) > 0 {
			return endpoints, src.Name(), nil
		}
	}

	// Priority 3: Auto-probe (fallback - silent failure allowed)
	if cfg.AutoProbe {
		src := NewProbeSource(cfg.BaseURL)
		endpoints, err := src.Discover(ctx)
		if err == nil && len(endpoints) > 0 {
			return endpoints, src.Name(), nil
		}
		// Silent fallback - don't return error
	}

	// If no sources configured, try auto-probe by default
	if cfg.OpenAPIPath == "" && cfg.ManualPath == "" && !cfg.AutoProbe {
		src := NewProbeSource(cfg.BaseURL)
		endpoints, err := src.Discover(ctx)
		if err == nil && len(endpoints) > 0 {
			return endpoints, src.Name(), nil
		}
	}

	return nil, "", fmt.Errorf("no endpoints discovered from any source")
}
