package discovery

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// OpenAPISource discovers endpoints from an OpenAPI/Swagger specification.
type OpenAPISource struct {
	specPath string
	baseURL  string
}

// NewOpenAPISource creates a new OpenAPI discovery source.
// specPath can be a local file path or a URL.
func NewOpenAPISource(specPath, baseURL string) *OpenAPISource {
	return &OpenAPISource{
		specPath: specPath,
		baseURL:  baseURL,
	}
}

// Name returns the source name.
func (s *OpenAPISource) Name() string {
	return fmt.Sprintf("openapi:%s", s.specPath)
}

// Discover parses the OpenAPI spec and returns discovered endpoints.
func (s *OpenAPISource) Discover(ctx context.Context) ([]RawEndpoint, error) {
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	loader.Context = ctx

	var doc *openapi3.T
	var err error

	// Determine if specPath is a URL or file
	if isURL(s.specPath) {
		// Parse the URL
		specURL, parseErr := url.Parse(s.specPath)
		if parseErr != nil {
			return nil, fmt.Errorf("failed to parse OpenAPI URL %s: %w", s.specPath, parseErr)
		}

		// Load from URL
		doc, err = loader.LoadFromURI(specURL)
		if err != nil {
			return nil, fmt.Errorf("failed to load OpenAPI spec from URL %s: %w", s.specPath, err)
		}
	} else {
		// Load from file
		data, err := os.ReadFile(s.specPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read OpenAPI spec file %s: %w", s.specPath, err)
		}
		doc, err = loader.LoadFromData(data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse OpenAPI spec: %w", err)
		}
	}

	// Skip strict validation - many real-world specs have minor issues
	// (e.g., OpenAPI 3.1 features like type: null not fully supported).
	// We're a runtime tester, so actual HTTP behavior matters more than spec compliance.

	return s.extractEndpoints(doc), nil
}

// extractEndpoints converts OpenAPI paths to RawEndpoints.
func (s *OpenAPISource) extractEndpoints(doc *openapi3.T) []RawEndpoint {
	var endpoints []RawEndpoint

	for path, pathItem := range doc.Paths.Map() {
		// Process each HTTP method
		for method, operation := range pathItem.Operations() {
			if operation == nil {
				continue
			}

			endpoint := RawEndpoint{
				Path:        path,
				Method:      strings.ToUpper(method),
				OperationID: operation.OperationID,
				Summary:     operation.Summary,
				Parameters:  s.extractParameters(pathItem.Parameters, operation.Parameters),
				RequestBody: s.extractRequestBody(operation.RequestBody),
				Security:    s.extractSecurity(operation.Security, doc.Security),
				Responses:   s.extractResponses(operation.Responses),
			}

			endpoints = append(endpoints, endpoint)
		}
	}

	return endpoints
}

// extractParameters combines path-level and operation-level parameters.
func (s *OpenAPISource) extractParameters(pathParams, opParams openapi3.Parameters) []RawParameter {
	var params []RawParameter
	seen := make(map[string]bool)

	// Operation parameters take precedence
	for _, ref := range opParams {
		if ref == nil || ref.Value == nil {
			continue
		}
		p := ref.Value
		key := p.In + ":" + p.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		params = append(params, s.convertParameter(p))
	}

	// Add path-level parameters not overridden by operation
	for _, ref := range pathParams {
		if ref == nil || ref.Value == nil {
			continue
		}
		p := ref.Value
		key := p.In + ":" + p.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		params = append(params, s.convertParameter(p))
	}

	return params
}

// convertParameter converts an OpenAPI parameter to RawParameter.
func (s *OpenAPISource) convertParameter(p *openapi3.Parameter) RawParameter {
	param := RawParameter{
		Name:     p.Name,
		In:       p.In,
		Required: p.Required,
	}

	if p.Schema != nil && p.Schema.Value != nil {
		types := p.Schema.Value.Type.Slice()
		if len(types) > 0 {
			param.Type = types[0]
		} else {
			param.Type = "string" // Default to string if type not specified
		}
		param.Default = p.Schema.Value.Default
		param.Example = p.Schema.Value.Example
	}

	// Override example from parameter level if present
	if p.Example != nil {
		param.Example = p.Example
	}

	return param
}

// extractRequestBody extracts request body specification.
func (s *OpenAPISource) extractRequestBody(body *openapi3.RequestBodyRef) *RawRequestBody {
	if body == nil || body.Value == nil {
		return nil
	}

	rb := body.Value

	// Prefer JSON content type
	for contentType, mediaType := range rb.Content {
		if strings.Contains(contentType, "json") {
			result := &RawRequestBody{
				Required:    rb.Required,
				ContentType: contentType,
			}

			if mediaType.Schema != nil && mediaType.Schema.Value != nil {
				result.Schema = schemaToMap(mediaType.Schema.Value)
			}

			if mediaType.Example != nil {
				result.Example = mediaType.Example
			}

			return result
		}
	}

	// Fall back to first content type
	for contentType, mediaType := range rb.Content {
		result := &RawRequestBody{
			Required:    rb.Required,
			ContentType: contentType,
		}

		if mediaType.Schema != nil && mediaType.Schema.Value != nil {
			result.Schema = schemaToMap(mediaType.Schema.Value)
		}

		if mediaType.Example != nil {
			result.Example = mediaType.Example
		}

		return result
	}

	return nil
}

// extractSecurity extracts security requirements.
func (s *OpenAPISource) extractSecurity(opSecurity *openapi3.SecurityRequirements, globalSecurity openapi3.SecurityRequirements) []RawSecurityReq {
	var reqs []RawSecurityReq

	// Operation-level security overrides global
	security := globalSecurity
	if opSecurity != nil {
		security = *opSecurity
	}

	for _, req := range security {
		for name, scopes := range req {
			reqs = append(reqs, RawSecurityReq{
				Name:   name,
				Scopes: scopes,
			})
		}
	}

	return reqs
}

// extractResponses extracts response status codes and descriptions.
func (s *OpenAPISource) extractResponses(responses *openapi3.Responses) map[string]string {
	result := make(map[string]string)

	if responses == nil {
		return result
	}

	for status, ref := range responses.Map() {
		if ref != nil && ref.Value != nil {
			desc := ""
			if ref.Value.Description != nil {
				desc = *ref.Value.Description
			}
			result[status] = desc
		}
	}

	return result
}

// schemaToMap converts an OpenAPI schema to a simple map representation.
func schemaToMap(schema *openapi3.Schema) map[string]any {
	if schema == nil {
		return nil
	}

	result := make(map[string]any)

	if len(schema.Type.Slice()) > 0 {
		result["type"] = schema.Type.Slice()[0]
	}

	if schema.Properties != nil {
		props := make(map[string]any)
		for name, propRef := range schema.Properties {
			if propRef != nil && propRef.Value != nil {
				props[name] = schemaToMap(propRef.Value)
			}
		}
		result["properties"] = props
	}

	if len(schema.Required) > 0 {
		result["required"] = schema.Required
	}

	if schema.Items != nil && schema.Items.Value != nil {
		result["items"] = schemaToMap(schema.Items.Value)
	}

	return result
}

// isURL checks if a string looks like a URL.
func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}
