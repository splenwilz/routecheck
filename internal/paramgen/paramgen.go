// Package paramgen generates parameter values for API testing.
// This is Layer 4 of the pipeline - it produces safe test values.
// Priority: Example > Default > Inferred from name/type
package paramgen

import (
	"fmt"
	"strings"

	"github.com/splenwilz/routecheck/pkg/endpoint"
)

// GeneratedParams holds generated values for an endpoint request.
type GeneratedParams struct {
	PathParams  map[string]string
	QueryParams map[string]string
	Headers     map[string]string
	Body        any
}

// Generate creates parameter values for an endpoint.
// It uses examples/defaults when available, otherwise infers safe values.
func Generate(ep endpoint.Endpoint) GeneratedParams {
	return GeneratedParams{
		PathParams:  generateParams(ep.PathParams),
		QueryParams: generateParams(ep.QueryParams),
		Headers:     generateParams(ep.Headers),
		Body:        generateBody(ep.Body),
	}
}

// generateParams generates values for a slice of parameters.
func generateParams(params []endpoint.Param) map[string]string {
	result := make(map[string]string)

	for _, p := range params {
		// Only generate required params by default (safe approach)
		if !p.Required {
			continue
		}

		value := generateValue(p)
		if value != "" {
			result[p.Name] = value
		}
	}

	return result
}

// generateValue generates a single parameter value.
// Priority: Example > Default > Inferred
func generateValue(p endpoint.Param) string {
	// Priority 1: Use example if available
	if p.Example != nil && *p.Example != "" {
		return *p.Example
	}

	// Priority 2: Use default if available
	if p.Default != nil && *p.Default != "" {
		return *p.Default
	}

	// Priority 3: Infer from name and type
	return inferValue(p.Name, p.Type)
}

// inferValue generates a safe value based on parameter name and type.
func inferValue(name, paramType string) string {
	lower := strings.ToLower(name)

	// Common ID patterns
	if strings.Contains(lower, "id") {
		return "1"
	}

	// Common name patterns
	if strings.Contains(lower, "name") {
		return "test"
	}

	// Email patterns
	if strings.Contains(lower, "email") {
		return "test@example.com"
	}

	// Pagination patterns
	if lower == "page" || lower == "page_number" || lower == "pagenumber" {
		return "1"
	}
	if lower == "limit" || lower == "per_page" || lower == "perpage" || lower == "size" || lower == "pagesize" {
		return "10"
	}
	if lower == "offset" || lower == "skip" {
		return "0"
	}

	// Sort/order patterns
	if lower == "sort" || lower == "order" || lower == "orderby" || lower == "order_by" {
		return "id"
	}
	if lower == "direction" || lower == "sort_order" || lower == "sortorder" {
		return "asc"
	}

	// Boolean patterns
	if strings.Contains(lower, "active") || strings.Contains(lower, "enabled") {
		return "true"
	}

	// Date patterns
	if strings.Contains(lower, "date") || strings.Contains(lower, "time") {
		return "2024-01-01"
	}

	// Fallback based on type
	return inferFromType(paramType)
}

// inferFromType generates a value based solely on the parameter type.
func inferFromType(paramType string) string {
	switch paramType {
	case "integer", "int":
		return "1"
	case "number", "float", "double":
		return "1.0"
	case "boolean", "bool":
		return "true"
	case "array":
		return ""
	case "object":
		return ""
	default:
		return "test"
	}
}

// generateBody generates a request body from the spec.
// Returns nil for GET requests or when no body is expected.
func generateBody(spec *endpoint.BodySpec) any {
	if spec == nil {
		return nil
	}

	// If we have an example, use it
	if spec.Example != nil {
		return spec.Example
	}

	// Generate from schema
	if spec.Schema != nil {
		return generateFromSchema(spec.Schema)
	}

	return nil
}

// generateFromSchema generates a body from a JSON schema.
func generateFromSchema(schema map[string]any) map[string]any {
	result := make(map[string]any)

	schemaType, _ := schema["type"].(string)
	if schemaType != "object" && schemaType != "" {
		return nil
	}

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return result
	}

	required := make(map[string]bool)
	if reqList, ok := schema["required"].([]any); ok {
		for _, r := range reqList {
			if s, ok := r.(string); ok {
				required[s] = true
			}
		}
	}

	// Only generate required fields (safe approach)
	for name, propSchema := range properties {
		if !required[name] {
			continue
		}

		prop, ok := propSchema.(map[string]any)
		if !ok {
			continue
		}

		result[name] = generatePropertyValue(name, prop)
	}

	return result
}

// generatePropertyValue generates a value for a schema property.
func generatePropertyValue(name string, prop map[string]any) any {
	// Check for example
	if example, ok := prop["example"]; ok {
		return example
	}

	// Check for default
	if def, ok := prop["default"]; ok {
		return def
	}

	propType, _ := prop["type"].(string)

	// Infer from name first
	value := inferValue(name, propType)
	if value != "" && value != "test" {
		// Convert to appropriate type
		switch propType {
		case "integer":
			return 1
		case "number":
			return 1.0
		case "boolean":
			return true
		default:
			return value
		}
	}

	// Fallback based on type
	switch propType {
	case "string":
		// Check for format
		if format, ok := prop["format"].(string); ok {
			return generateForFormat(format)
		}
		return "test"
	case "integer":
		return 1
	case "number":
		return 1.0
	case "boolean":
		return true
	case "array":
		return []any{}
	case "object":
		if nested, ok := prop["properties"].(map[string]any); ok {
			return generateFromSchema(map[string]any{
				"type":       "object",
				"properties": nested,
			})
		}
		return map[string]any{}
	default:
		return "test"
	}
}

// generateForFormat generates a value for a specific string format.
func generateForFormat(format string) string {
	switch format {
	case "email":
		return "test@example.com"
	case "uri", "url":
		return "https://example.com"
	case "uuid":
		return "00000000-0000-0000-0000-000000000001"
	case "date":
		return "2024-01-01"
	case "date-time":
		return "2024-01-01T00:00:00Z"
	case "time":
		return "00:00:00"
	case "hostname":
		return "example.com"
	case "ipv4":
		return "127.0.0.1"
	case "ipv6":
		return "::1"
	default:
		return "test"
	}
}

// BuildPath substitutes path parameters into a URL path.
func BuildPath(path string, params map[string]string) string {
	result := path
	for name, value := range params {
		// Handle both {param} and :param styles
		result = strings.ReplaceAll(result, "{"+name+"}", value)
		result = strings.ReplaceAll(result, ":"+name, value)
	}
	return result
}

// BuildQueryString creates a query string from parameters.
func BuildQueryString(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}

	parts := make([]string, 0, len(params))
	for name, value := range params {
		parts = append(parts, fmt.Sprintf("%s=%s", name, value))
	}

	return "?" + strings.Join(parts, "&")
}
