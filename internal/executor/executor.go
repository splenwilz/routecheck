// Package executor handles HTTP request execution.
// This is Layer 5 of the pipeline - it makes real HTTP requests.
package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/splenwilz/routecheck/internal/paramgen"
	"github.com/splenwilz/routecheck/pkg/auth"
	"github.com/splenwilz/routecheck/pkg/endpoint"
)

// Result represents the outcome of executing a single endpoint.
type Result struct {
	Endpoint   endpoint.Endpoint
	Request    RequestInfo
	Response   ResponseInfo
	Error      error
	Duration   time.Duration
	Retries    int
	SkipReason string // Non-empty if the endpoint was skipped
}

// RequestInfo captures details about the request made.
type RequestInfo struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    string
}

// ResponseInfo captures details about the response received.
type ResponseInfo struct {
	StatusCode int
	Status     string
	Headers    map[string]string
	Body       string
	BodySize   int
}

// Config holds executor configuration.
type Config struct {
	BaseURL     string
	Timeout     time.Duration
	Concurrency int
	SafeOnly    bool          // Only execute safe methods (GET, HEAD, OPTIONS)
	Auth        auth.Provider // Authentication provider (nil if none)
	MaxRetries  int           // Max retries for idempotent requests
	RetryDelay  time.Duration // Delay between retries
}

// Executor executes HTTP requests against endpoints.
type Executor struct {
	client *http.Client
	config Config
}

// New creates a new Executor with the given configuration.
func New(cfg Config) *Executor {
	return &Executor{
		client: &http.Client{
			Timeout: cfg.Timeout,
			// Don't follow redirects automatically - we want to report them
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				return http.ErrUseLastResponse
			},
		},
		config: cfg,
	}
}

// Execute runs requests for all endpoints and returns results.
// Uses a worker pool to limit concurrency.
func (e *Executor) Execute(ctx context.Context, endpoints []endpoint.Endpoint) []Result {
	results := make([]Result, len(endpoints))

	// Channel for work distribution
	work := make(chan int, len(endpoints))
	for i := range endpoints {
		work <- i
	}
	close(work)

	// Worker pool
	var wg sync.WaitGroup
	concurrency := e.config.Concurrency
	if concurrency <= 0 {
		concurrency = 5
	}

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range work {
				select {
				case <-ctx.Done():
					results[idx] = Result{
						Endpoint: endpoints[idx],
						Error:    ctx.Err(),
					}
				default:
					results[idx] = e.executeOne(ctx, endpoints[idx])
				}
			}
		}()
	}

	wg.Wait()
	return results
}

// executeOne executes a single endpoint request.
func (e *Executor) executeOne(ctx context.Context, ep endpoint.Endpoint) Result {
	result := Result{Endpoint: ep}

	// Safety check: skip non-safe methods if SafeOnly is set
	if e.config.SafeOnly && !ep.IsSafe() {
		result.SkipReason = fmt.Sprintf("unsafe method %s (use --unsafe to enable)", ep.Method)
		return result
	}

	// Generate parameters
	params := paramgen.Generate(ep)

	// Build URL
	path := paramgen.BuildPath(ep.Path, params.PathParams)
	query := paramgen.BuildQueryString(params.QueryParams)
	url := strings.TrimSuffix(e.config.BaseURL, "/") + path + query

	// Build request body
	var bodyReader io.Reader
	var bodyStr string
	if params.Body != nil {
		bodyBytes, err := json.Marshal(params.Body)
		if err != nil {
			result.Error = fmt.Errorf("failed to marshal request body: %w", err)
			return result
		}
		bodyStr = string(bodyBytes)
		bodyReader = bytes.NewReader(bodyBytes)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, ep.Method, url, bodyReader)
	if err != nil {
		result.Error = fmt.Errorf("failed to create request: %w", err)
		return result
	}

	// Set headers
	requestHeaders := make(map[string]string)

	// Content-Type for requests with body
	if bodyReader != nil {
		contentType := "application/json"
		if ep.Body != nil && ep.Body.ContentType != "" {
			contentType = ep.Body.ContentType
		}
		req.Header.Set("Content-Type", contentType)
		requestHeaders["Content-Type"] = contentType
	}

	// Accept header
	req.Header.Set("Accept", "application/json, */*")
	requestHeaders["Accept"] = "application/json, */*"

	// User-Agent
	req.Header.Set("User-Agent", "routecheck/1.0")
	requestHeaders["User-Agent"] = "routecheck/1.0"

	// Custom headers from params
	for name, value := range params.Headers {
		req.Header.Set(name, value)
		requestHeaders[name] = value
	}

	// Authentication
	e.applyAuth(req, requestHeaders)

	// Record request info
	result.Request = RequestInfo{
		Method:  ep.Method,
		URL:     url,
		Headers: requestHeaders,
		Body:    bodyStr,
	}

	// Execute with retries for idempotent methods
	maxAttempts := 1
	if ep.IsIdempotent() && e.config.MaxRetries > 0 {
		maxAttempts = e.config.MaxRetries + 1
	}

	var resp *http.Response
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			// Wait before retry
			select {
			case <-ctx.Done():
				result.Error = ctx.Err()
				return result
			case <-time.After(e.config.RetryDelay):
			}
			result.Retries++

			// Recreate request for retry (body reader is consumed)
			if bodyReader != nil {
				bodyReader = bytes.NewReader([]byte(bodyStr))
			}
			req, _ = http.NewRequestWithContext(ctx, ep.Method, url, bodyReader)
			for k, v := range requestHeaders {
				req.Header.Set(k, v)
			}
		}

		start := time.Now()
		resp, lastErr = e.client.Do(req)
		result.Duration = time.Since(start)

		if lastErr == nil {
			break
		}
	}

	if lastErr != nil {
		result.Error = lastErr
		return result
	}
	defer resp.Body.Close()

	// Read response body (with limit)
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024)) // 1MB limit
	if err != nil {
		result.Error = fmt.Errorf("failed to read response body: %w", err)
		return result
	}

	// Record response info
	responseHeaders := make(map[string]string)
	for k, v := range resp.Header {
		if len(v) > 0 {
			responseHeaders[k] = v[0]
		}
	}

	result.Response = ResponseInfo{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Headers:    responseHeaders,
		Body:       string(bodyBytes),
		BodySize:   len(bodyBytes),
	}

	return result
}

// applyAuth adds authentication to the request using the configured provider.
// It also records the auth header in the headers map for reporting (masked).
func (e *Executor) applyAuth(req *http.Request, headers map[string]string) {
	if e.config.Auth == nil {
		return
	}

	// Apply authentication to the request
	e.config.Auth.Apply(req)

	// Record auth header for reporting (will be masked by reporter)
	// Check common auth headers that were likely set
	if v := req.Header.Get("Authorization"); v != "" {
		headers["Authorization"] = v
	}
	// For API keys, check if a custom header was set
	if apiKey, ok := e.config.Auth.(*auth.APIKeyProvider); ok {
		headerName := apiKey.HeaderName()
		if v := req.Header.Get(headerName); v != "" {
			headers[headerName] = v
		}
	}
}
