// Package auth provides authentication providers for HTTP requests.
// All providers are stateless and safe for concurrent use.
package auth

import (
	"encoding/base64"
	"net/http"
)

// Provider injects authentication into HTTP requests.
type Provider interface {
	// Apply adds authentication headers to the request.
	Apply(req *http.Request)

	// Type returns the authentication type name.
	Type() string
}

// BearerProvider adds Bearer token authentication.
type BearerProvider struct {
	token string
}

// NewBearerProvider creates a provider for Bearer token auth.
func NewBearerProvider(token string) *BearerProvider {
	return &BearerProvider{token: token}
}

// Apply adds the Authorization: Bearer header.
func (p *BearerProvider) Apply(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+p.token)
}

// Type returns "bearer".
func (p *BearerProvider) Type() string {
	return "bearer"
}

// APIKeyProvider adds API key authentication.
type APIKeyProvider struct {
	key        string
	headerName string
}

// NewAPIKeyProvider creates a provider for API key auth.
// headerName specifies which header to use (e.g., "X-API-Key", "Authorization").
func NewAPIKeyProvider(key, headerName string) *APIKeyProvider {
	if headerName == "" {
		headerName = "X-API-Key"
	}
	return &APIKeyProvider{
		key:        key,
		headerName: headerName,
	}
}

// Apply adds the API key header.
func (p *APIKeyProvider) Apply(req *http.Request) {
	req.Header.Set(p.headerName, p.key)
}

// Type returns "api_key".
func (p *APIKeyProvider) Type() string {
	return "api_key"
}

// HeaderName returns the header name used for the API key.
func (p *APIKeyProvider) HeaderName() string {
	return p.headerName
}

// BasicProvider adds HTTP Basic authentication.
type BasicProvider struct {
	encoded string
}

// NewBasicProvider creates a provider for Basic auth.
func NewBasicProvider(username, password string) *BasicProvider {
	credentials := username + ":" + password
	encoded := base64.StdEncoding.EncodeToString([]byte(credentials))
	return &BasicProvider{encoded: encoded}
}

// Apply adds the Authorization: Basic header.
func (p *BasicProvider) Apply(req *http.Request) {
	req.Header.Set("Authorization", "Basic "+p.encoded)
}

// Type returns "basic".
func (p *BasicProvider) Type() string {
	return "basic"
}
