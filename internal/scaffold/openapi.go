package scaffold

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// OpenAPISpec represents a parsed OpenAPI specification.
// We only parse what we need for resource detection.
type OpenAPISpec struct {
	OpenAPI string                 `json:"openapi" yaml:"openapi"`
	Swagger string                 `json:"swagger" yaml:"swagger"` // OpenAPI 2.0
	Paths   map[string]PathItem    `json:"paths" yaml:"paths"`
	Info    map[string]interface{} `json:"info" yaml:"info"`
}

// PathItem represents operations on a path.
type PathItem struct {
	Get    *Operation `json:"get" yaml:"get"`
	Post   *Operation `json:"post" yaml:"post"`
	Put    *Operation `json:"put" yaml:"put"`
	Patch  *Operation `json:"patch" yaml:"patch"`
	Delete *Operation `json:"delete" yaml:"delete"`
}

// Operation represents an HTTP operation.
type Operation struct {
	Summary     string              `json:"summary" yaml:"summary"`
	OperationID string              `json:"operationId" yaml:"operationId"`
	Parameters  []Parameter         `json:"parameters" yaml:"parameters"`
	RequestBody *RequestBody        `json:"requestBody" yaml:"requestBody"`
	Responses   map[string]Response `json:"responses" yaml:"responses"`
}

// Parameter represents an operation parameter.
type Parameter struct {
	Name     string      `json:"name" yaml:"name"`
	In       string      `json:"in" yaml:"in"` // path, query, header, cookie
	Required bool        `json:"required" yaml:"required"`
	Schema   *SchemaRef  `json:"schema" yaml:"schema"`
}

// RequestBody represents a request body.
type RequestBody struct {
	Required bool               `json:"required" yaml:"required"`
	Content  map[string]Content `json:"content" yaml:"content"`
}

// Content represents media type content.
type Content struct {
	Schema *SchemaRef `json:"schema" yaml:"schema"`
}

// Response represents an operation response.
type Response struct {
	Description string `json:"description" yaml:"description"`
}

// SchemaRef represents a schema reference or inline schema.
type SchemaRef struct {
	Ref        string                `json:"$ref" yaml:"$ref"`
	Type       string                `json:"type" yaml:"type"`
	Properties map[string]*SchemaRef `json:"properties" yaml:"properties"`
	Required   []string              `json:"required" yaml:"required"`
}

// LoadOpenAPI loads an OpenAPI spec from a file or URL.
func LoadOpenAPI(path string) (*OpenAPISpec, error) {
	var data []byte
	var err error

	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		data, err = fetchURL(path)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}

	return parseOpenAPI(data, path)
}

// fetchURL fetches content from a URL.
func fetchURL(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch %s: status %d", url, resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// parseOpenAPI parses OpenAPI data (JSON or YAML).
func parseOpenAPI(data []byte, path string) (*OpenAPISpec, error) {
	var spec OpenAPISpec

	// Try YAML first (also handles JSON)
	if err := yaml.Unmarshal(data, &spec); err != nil {
		// Try JSON explicitly
		if jsonErr := json.Unmarshal(data, &spec); jsonErr != nil {
			return nil, fmt.Errorf("invalid OpenAPI spec: %w", err)
		}
	}

	// Validate it's an OpenAPI spec
	if spec.OpenAPI == "" && spec.Swagger == "" {
		return nil, fmt.Errorf("invalid OpenAPI spec: missing 'openapi' or 'swagger' version field")
	}

	if len(spec.Paths) == 0 {
		return nil, fmt.Errorf("invalid OpenAPI spec: no paths defined")
	}

	return &spec, nil
}

// ExtractEndpoints extracts all endpoints from the spec.
func (s *OpenAPISpec) ExtractEndpoints() []Endpoint {
	var endpoints []Endpoint

	for path, item := range s.Paths {
		if item.Get != nil {
			endpoints = append(endpoints, s.operationToEndpoint("GET", path, item.Get))
		}
		if item.Post != nil {
			endpoints = append(endpoints, s.operationToEndpoint("POST", path, item.Post))
		}
		if item.Put != nil {
			endpoints = append(endpoints, s.operationToEndpoint("PUT", path, item.Put))
		}
		if item.Patch != nil {
			endpoints = append(endpoints, s.operationToEndpoint("PATCH", path, item.Patch))
		}
		if item.Delete != nil {
			endpoints = append(endpoints, s.operationToEndpoint("DELETE", path, item.Delete))
		}
	}

	return endpoints
}

// operationToEndpoint converts an Operation to an Endpoint.
func (s *OpenAPISpec) operationToEndpoint(method, path string, op *Operation) Endpoint {
	ep := Endpoint{
		Method:  method,
		Path:    path,
		Summary: op.Summary,
	}

	// Extract ID parameter from path
	ep.IDParam = extractIDParam(path, op.Parameters)

	// Extract response status codes
	for code := range op.Responses {
		if status := parseStatusCode(code); status > 0 {
			ep.StatusCodes = append(ep.StatusCodes, status)
		}
	}

	// Extract body fields from request body schema
	if op.RequestBody != nil {
		ep.BodyFields = extractBodyFields(op.RequestBody)
	}

	return ep
}

// extractIDParam extracts the ID parameter name from path and parameters.
func extractIDParam(path string, params []Parameter) string {
	// Look for {param} patterns in path
	for _, param := range params {
		if param.In == "path" {
			// Check if it looks like an ID parameter
			name := strings.ToLower(param.Name)
			if name == "id" || strings.HasSuffix(name, "id") || strings.HasSuffix(name, "_id") {
				return param.Name
			}
		}
	}

	// Fallback: extract from path template
	parts := strings.Split(path, "/")
	for _, part := range parts {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			name := part[1 : len(part)-1]
			lower := strings.ToLower(name)
			if lower == "id" || strings.HasSuffix(lower, "id") || strings.HasSuffix(lower, "_id") {
				return name
			}
		}
	}

	return ""
}

// extractBodyFields extracts fields from request body schema.
func extractBodyFields(body *RequestBody) []BodyField {
	var fields []BodyField

	// Look for JSON content
	for contentType, content := range body.Content {
		if strings.Contains(contentType, "json") && content.Schema != nil {
			fields = extractSchemaFields(content.Schema)
			break
		}
	}

	return fields
}

// extractSchemaFields extracts fields from a schema.
func extractSchemaFields(schema *SchemaRef) []BodyField {
	var fields []BodyField

	if schema.Properties == nil {
		return fields
	}

	// Build required map
	required := make(map[string]bool)
	for _, name := range schema.Required {
		required[name] = true
	}

	for name, prop := range schema.Properties {
		field := BodyField{
			Name:     name,
			Type:     prop.Type,
			Required: required[name],
		}
		if field.Type == "" {
			field.Type = "unknown"
		}
		fields = append(fields, field)
	}

	return fields
}

// parseStatusCode parses a status code string to int.
func parseStatusCode(code string) int {
	var status int
	fmt.Sscanf(code, "%d", &status)
	return status
}
