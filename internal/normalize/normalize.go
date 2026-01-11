// Package normalize converts raw discovered endpoints to the canonical model.
// This is Layer 3 of the pipeline - all endpoints become uniform after this.
// No source-specific logic is allowed beyond this point.
package normalize

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/splenwilz/routecheck/internal/discovery"
	"github.com/splenwilz/routecheck/pkg/endpoint"
)

// Normalize converts raw discovered endpoints to canonical endpoints.
func Normalize(raw []discovery.RawEndpoint) []endpoint.Endpoint {
	endpoints := make([]endpoint.Endpoint, 0, len(raw))

	for _, r := range raw {
		endpoints = append(endpoints, normalizeEndpoint(r))
	}

	return endpoints
}

// normalizeEndpoint converts a single RawEndpoint to canonical Endpoint.
func normalizeEndpoint(raw discovery.RawEndpoint) endpoint.Endpoint {
	ep := endpoint.Endpoint{
		Path:           raw.Path,
		Method:         strings.ToUpper(raw.Method),
		PathParams:     extractParams(raw.Parameters, "path"),
		QueryParams:    extractParams(raw.Parameters, "query"),
		Headers:        extractParams(raw.Parameters, "header"),
		Body:           normalizeBody(raw.RequestBody),
		Auth:           normalizeAuth(raw.Security),
		ExpectedStatus: extractExpectedStatus(raw.Responses),
	}

	return ep
}

// extractParams filters and converts parameters by location.
func extractParams(params []discovery.RawParameter, location string) []endpoint.Param {
	var result []endpoint.Param

	for _, p := range params {
		if p.In != location {
			continue
		}

		param := endpoint.Param{
			Name:     p.Name,
			Type:     normalizeType(p.Type),
			Required: p.Required,
		}

		if p.Default != nil {
			s := fmt.Sprintf("%v", p.Default)
			param.Default = &s
		}

		if p.Example != nil {
			s := fmt.Sprintf("%v", p.Example)
			param.Example = &s
		}

		result = append(result, param)
	}

	return result
}

// normalizeType normalizes type strings to canonical types.
func normalizeType(t string) string {
	switch strings.ToLower(t) {
	case "integer", "int", "int32", "int64":
		return "integer"
	case "number", "float", "double":
		return "number"
	case "boolean", "bool":
		return "boolean"
	case "array":
		return "array"
	case "object":
		return "object"
	case "string", "":
		return "string"
	default:
		return "string"
	}
}

// normalizeBody converts RawRequestBody to canonical BodySpec.
func normalizeBody(raw *discovery.RawRequestBody) *endpoint.BodySpec {
	if raw == nil {
		return nil
	}

	return &endpoint.BodySpec{
		ContentType: raw.ContentType,
		Schema:      raw.Schema,
		Example:     raw.Example,
	}
}

// normalizeAuth converts security requirements to canonical AuthRequirement.
// Takes the first security requirement found.
func normalizeAuth(security []discovery.RawSecurityReq) *endpoint.AuthRequirement {
	if len(security) == 0 {
		return nil
	}

	// Use the first security requirement
	sec := security[0]

	// Infer auth type from name (common patterns)
	authType := inferAuthType(sec.Name)

	return &endpoint.AuthRequirement{
		Type: authType,
		In:   inferAuthLocation(authType),
		Name: inferAuthHeaderName(authType, sec.Name),
	}
}

// inferAuthType guesses the auth type from the security scheme name.
func inferAuthType(name string) string {
	lower := strings.ToLower(name)

	switch {
	case strings.Contains(lower, "bearer") || strings.Contains(lower, "jwt"):
		return "bearer"
	case strings.Contains(lower, "basic"):
		return "basic"
	case strings.Contains(lower, "api") && strings.Contains(lower, "key"):
		return "api_key"
	case strings.Contains(lower, "oauth"):
		return "oauth2"
	default:
		// Default to bearer as it's most common for REST APIs
		return "bearer"
	}
}

// inferAuthLocation returns where the auth should be sent.
func inferAuthLocation(authType string) string {
	switch authType {
	case "api_key":
		return "header" // Could also be "query", but header is more common
	default:
		return "header"
	}
}

// inferAuthHeaderName returns the header name for the auth type.
func inferAuthHeaderName(authType, schemeName string) string {
	switch authType {
	case "bearer", "basic", "oauth2":
		return "Authorization"
	case "api_key":
		// Use scheme name as header name for API keys
		if schemeName != "" {
			return schemeName
		}
		return "X-API-Key"
	default:
		return "Authorization"
	}
}

// extractExpectedStatus extracts expected success status codes from responses.
// If no 2xx status codes are defined, returns empty (caller uses default: any 2xx).
func extractExpectedStatus(responses map[string]string) []int {
	var statuses []int

	for code := range responses {
		// Handle "default" and other non-numeric codes
		if code == "default" {
			continue
		}

		status, err := strconv.Atoi(code)
		if err != nil {
			continue
		}

		// Only include success status codes (2xx)
		if status >= 200 && status < 300 {
			statuses = append(statuses, status)
		}
	}

	return statuses
}
