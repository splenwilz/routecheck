package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ManualMap represents the structure of a manual endpoint map file.
type ManualMap struct {
	BaseURL   string           `json:"base_url" yaml:"base_url"`
	Endpoints []ManualEndpoint `json:"endpoints" yaml:"endpoints"`
}

// ManualEndpoint represents a single endpoint in the manual map.
type ManualEndpoint struct {
	Method         string              `json:"method" yaml:"method"`
	Path           string              `json:"path" yaml:"path"`
	Params         *ManualParams       `json:"params,omitempty" yaml:"params,omitempty"`
	Body           *ManualBody         `json:"body,omitempty" yaml:"body,omitempty"`
	Headers        map[string]string   `json:"headers,omitempty" yaml:"headers,omitempty"`
	Auth           string              `json:"auth,omitempty" yaml:"auth,omitempty"`
	ExpectedStatus []int               `json:"expected_status,omitempty" yaml:"expected_status,omitempty"`
}

// ManualParams holds parameter definitions.
type ManualParams struct {
	Path  map[string]string `json:"path,omitempty" yaml:"path,omitempty"`
	Query map[string]string `json:"query,omitempty" yaml:"query,omitempty"`
}

// ManualBody holds request body definition.
type ManualBody struct {
	ContentType string `json:"content_type,omitempty" yaml:"content_type,omitempty"`
	Example     any    `json:"example,omitempty" yaml:"example,omitempty"`
}

// ManualSource discovers endpoints from a manual YAML/JSON map file.
type ManualSource struct {
	filePath string
	baseURL  string
}

// NewManualSource creates a new manual map discovery source.
func NewManualSource(filePath, baseURL string) *ManualSource {
	return &ManualSource{
		filePath: filePath,
		baseURL:  baseURL,
	}
}

// Name returns the source name.
func (s *ManualSource) Name() string {
	return fmt.Sprintf("manual:%s", filepath.Base(s.filePath))
}

// Discover parses the manual map file and returns discovered endpoints.
func (s *ManualSource) Discover(ctx context.Context) ([]RawEndpoint, error) {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read manual map file: %w", err)
	}

	manualMap, err := s.parseFile(data)
	if err != nil {
		return nil, err
	}

	if err := s.validate(manualMap); err != nil {
		return nil, err
	}

	return s.convertToRaw(manualMap), nil
}

// parseFile parses YAML or JSON based on file extension.
func (s *ManualSource) parseFile(data []byte) (*ManualMap, error) {
	var manualMap ManualMap

	ext := strings.ToLower(filepath.Ext(s.filePath))

	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &manualMap); err != nil {
			return nil, fmt.Errorf("failed to parse YAML: %w", err)
		}
	case ".json":
		if err := json.Unmarshal(data, &manualMap); err != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w", err)
		}
	default:
		// Try YAML first (superset of JSON), then JSON
		if err := yaml.Unmarshal(data, &manualMap); err != nil {
			if err := json.Unmarshal(data, &manualMap); err != nil {
				return nil, fmt.Errorf("failed to parse file (tried YAML and JSON): invalid format")
			}
		}
	}

	return &manualMap, nil
}

// validate checks the manual map for required fields and valid values.
func (s *ManualSource) validate(m *ManualMap) error {
	if len(m.Endpoints) == 0 {
		return fmt.Errorf("manual map has no endpoints defined")
	}

	validMethods := map[string]bool{
		"GET": true, "POST": true, "PUT": true, "PATCH": true,
		"DELETE": true, "HEAD": true, "OPTIONS": true,
	}

	for i, ep := range m.Endpoints {
		if ep.Path == "" {
			return fmt.Errorf("endpoint %d: path is required", i+1)
		}

		if ep.Method == "" {
			return fmt.Errorf("endpoint %d (%s): method is required", i+1, ep.Path)
		}

		method := strings.ToUpper(ep.Method)
		if !validMethods[method] {
			return fmt.Errorf("endpoint %d (%s): invalid method %q", i+1, ep.Path, ep.Method)
		}
	}

	return nil
}

// convertToRaw converts ManualMap to RawEndpoint slice.
func (s *ManualSource) convertToRaw(m *ManualMap) []RawEndpoint {
	endpoints := make([]RawEndpoint, 0, len(m.Endpoints))

	for _, ep := range m.Endpoints {
		raw := RawEndpoint{
			Path:   ep.Path,
			Method: strings.ToUpper(ep.Method),
		}

		// Convert parameters
		if ep.Params != nil {
			for name, typ := range ep.Params.Path {
				raw.Parameters = append(raw.Parameters, RawParameter{
					Name:     name,
					In:       "path",
					Type:     typ,
					Required: true, // Path params are always required
				})
			}
			for name, typ := range ep.Params.Query {
				raw.Parameters = append(raw.Parameters, RawParameter{
					Name:     name,
					In:       "query",
					Type:     typ,
					Required: false,
				})
			}
		}

		// Convert headers
		for name, value := range ep.Headers {
			raw.Parameters = append(raw.Parameters, RawParameter{
				Name:    name,
				In:      "header",
				Type:    "string",
				Example: value,
			})
		}

		// Convert body
		if ep.Body != nil {
			contentType := ep.Body.ContentType
			if contentType == "" {
				contentType = "application/json"
			}
			raw.RequestBody = &RawRequestBody{
				Required:    true,
				ContentType: contentType,
				Example:     ep.Body.Example,
			}
		}

		// Convert auth
		if ep.Auth != "" {
			raw.Security = append(raw.Security, RawSecurityReq{
				Name: ep.Auth,
			})
		}

		// Convert expected status
		if len(ep.ExpectedStatus) > 0 {
			raw.Responses = make(map[string]string)
			for _, status := range ep.ExpectedStatus {
				raw.Responses[fmt.Sprintf("%d", status)] = "Expected"
			}
		}

		endpoints = append(endpoints, raw)
	}

	return endpoints
}
