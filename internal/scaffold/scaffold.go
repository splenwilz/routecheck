// Package scaffold generates draft lifecycle specs from OpenAPI.
package scaffold

import (
	"fmt"
	"time"
)

// Config holds configuration for the scaffold command.
type Config struct {
	OpenAPIPath  string // Path or URL to OpenAPI spec
	ResourceName string // Specific resource to scaffold (optional)
	All          bool   // Scaffold all detected resources
	OutputPath   string // Output file path
}

// Result holds the scaffolding result.
type Result struct {
	Resources []ResourceGroup
	YAML      string
	Warnings  []string
}

// ResourceGroup represents a detected API resource with its endpoints.
type ResourceGroup struct {
	Name           string
	BasePath       string
	CreateEndpoint *Endpoint
	ReadEndpoint   *Endpoint
	DeleteEndpoint *Endpoint
	Warnings       []string
}

// Endpoint represents a detected API endpoint.
type Endpoint struct {
	Method      string
	Path        string
	IDParam     string            // e.g., "id", "userId"
	BodyFields  []BodyField       // Detected from schema
	StatusCodes []int             // Response status codes
	Summary     string            // From OpenAPI
}

// BodyField represents a field in the request body schema.
type BodyField struct {
	Name     string
	Type     string
	Required bool
}

// Scaffold generates a draft lifecycle spec from an OpenAPI spec.
func Scaffold(cfg Config) (*Result, error) {
	// Validate configuration
	if cfg.OpenAPIPath == "" {
		return nil, fmt.Errorf("--openapi is required")
	}
	if cfg.ResourceName == "" && !cfg.All {
		return nil, fmt.Errorf("specify --resource <name> or --all")
	}
	if cfg.ResourceName != "" && cfg.All {
		return nil, fmt.Errorf("--resource and --all are mutually exclusive")
	}

	// Load and parse OpenAPI spec
	spec, err := LoadOpenAPI(cfg.OpenAPIPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read OpenAPI spec: %w", err)
	}

	// Detect resource groups
	groups, err := DetectResources(spec)
	if err != nil {
		return nil, err
	}

	if len(groups) == 0 {
		return nil, fmt.Errorf("no lifecycle candidates found in spec")
	}

	// Filter by resource name if specified
	if cfg.ResourceName != "" {
		filtered := filterByResource(groups, cfg.ResourceName)
		if len(filtered) == 0 {
			return nil, fmt.Errorf("no endpoints found for resource %q. Available: %s",
				cfg.ResourceName, availableResources(groups))
		}
		groups = filtered
	}

	// Generate YAML
	yaml := GenerateYAML(groups, cfg.OpenAPIPath, time.Now())

	result := &Result{
		Resources: groups,
		YAML:      yaml,
	}

	// Collect warnings
	for _, g := range groups {
		result.Warnings = append(result.Warnings, g.Warnings...)
	}

	return result, nil
}

// filterByResource filters groups to only those matching the resource name.
func filterByResource(groups []ResourceGroup, name string) []ResourceGroup {
	var filtered []ResourceGroup
	for _, g := range groups {
		if g.Name == name {
			filtered = append(filtered, g)
		}
	}
	return filtered
}

// availableResources returns a comma-separated list of resource names.
func availableResources(groups []ResourceGroup) string {
	if len(groups) == 0 {
		return "(none)"
	}
	names := make([]string, len(groups))
	for i, g := range groups {
		names[i] = g.Name
	}
	result := names[0]
	for i := 1; i < len(names); i++ {
		result += ", " + names[i]
	}
	return result
}
