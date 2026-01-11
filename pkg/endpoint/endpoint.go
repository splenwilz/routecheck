// Package endpoint defines the canonical representation of API endpoints.
// All discovery sources MUST normalize to these structures.
// No framework-specific logic is allowed beyond this point.
package endpoint

// Endpoint is the canonical representation of an API endpoint.
// This is the shared contract between all layers in the pipeline.
type Endpoint struct {
	// Path is the URL path pattern (e.g., "/users/{id}")
	Path string

	// Method is the HTTP method (GET, POST, PUT, PATCH, DELETE, etc.)
	Method string

	// PathParams are parameters embedded in the path
	PathParams []Param

	// QueryParams are query string parameters
	QueryParams []Param

	// Headers are required or optional HTTP headers
	Headers []Param

	// Body specifies the request body (nil if none expected)
	Body *BodySpec

	// Auth specifies authentication requirements (nil if none)
	Auth *AuthRequirement

	// ExpectedStatus lists acceptable success status codes.
	// Default interpretation: any 2xx is success if empty.
	ExpectedStatus []int
}

// Param represents a single parameter (path, query, or header).
type Param struct {
	Name     string  // Parameter name
	Type     string  // Type: "string", "integer", "boolean", "number", "array", "object"
	Required bool    // Whether this parameter is required
	Default  *string // Default value if known (nil if none)
	Example  *string // Example value from spec (nil if none)
}

// BodySpec describes the expected request body.
type BodySpec struct {
	ContentType string         // MIME type (e.g., "application/json")
	Schema      map[string]any // JSON Schema or simplified representation
	Example     any            // Example body if available from spec
}

// AuthRequirement describes how authentication should be provided.
type AuthRequirement struct {
	Type string // Auth type: "bearer", "api_key", "basic", "oauth2"
	In   string // Where to send: "header", "query", "cookie"
	Name string // Header/param/cookie name (e.g., "Authorization", "X-API-Key")
}

// IsSafe returns true if the HTTP method is considered safe (no side effects).
// Safe methods: GET, HEAD, OPTIONS, TRACE
func (e *Endpoint) IsSafe() bool {
	switch e.Method {
	case "GET", "HEAD", "OPTIONS", "TRACE":
		return true
	default:
		return false
	}
}

// IsIdempotent returns true if the HTTP method is idempotent.
// Idempotent methods: GET, HEAD, OPTIONS, TRACE, PUT, DELETE
func (e *Endpoint) IsIdempotent() bool {
	switch e.Method {
	case "GET", "HEAD", "OPTIONS", "TRACE", "PUT", "DELETE":
		return true
	default:
		return false
	}
}

// IsSuccess checks if a status code is considered successful for this endpoint.
// If ExpectedStatus is empty, any 2xx status is considered success.
func (e *Endpoint) IsSuccess(statusCode int) bool {
	if len(e.ExpectedStatus) == 0 {
		return statusCode >= 200 && statusCode < 300
	}
	for _, expected := range e.ExpectedStatus {
		if statusCode == expected {
			return true
		}
	}
	return false
}
