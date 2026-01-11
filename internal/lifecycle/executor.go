// Package lifecycle handles lifecycle testing execution.
// Phase 3: CREATE → READ → UPDATE → CLEANUP.
package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/splenwilz/routecheck/pkg/auth"
)

// ExecutorConfig holds configuration for lifecycle execution.
type ExecutorConfig struct {
	BaseURL string
	Timeout time.Duration
	Auth    auth.Provider
}

// LifecycleExecutor executes lifecycle test sequences.
type LifecycleExecutor struct {
	client *http.Client
	config ExecutorConfig
}

// NewExecutor creates a new lifecycle executor.
func NewExecutor(cfg ExecutorConfig) *LifecycleExecutor {
	return &LifecycleExecutor{
		client: &http.Client{
			Timeout: cfg.Timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		config: cfg,
	}
}

// StepResult represents the result of executing a lifecycle step.
type StepResult struct {
	StepName   string
	Method     string
	Path       string            // Original path (may contain {{variables}})
	ActualPath string            // Interpolated path (actual URL path used)
	StatusCode int
	Duration   time.Duration
	Body       string
	Error      error
	Captured   map[string]string // Captured values from this step
}

// LifecycleResult represents the result of executing a full lifecycle.
type LifecycleResult struct {
	Name           string
	Steps          []StepResult
	CapturedVars   map[string]string // All captured variables
	CreateError    error             // Error from CREATE step
	ReadError      error             // Error from READ step (optional)
	UpdateError    error             // Error from UPDATE step (optional)
	CleanupError   error             // Error from CLEANUP step
	CleanupSkipped bool              // True if cleanup was skipped
}

// HasError returns true if any step failed.
func (r *LifecycleResult) HasError() bool {
	return r.CreateError != nil || r.ReadError != nil || r.UpdateError != nil || r.CleanupError != nil
}

// Execute runs the full lifecycle: CREATE → READ → UPDATE → CLEANUP.
// READ and UPDATE are optional (only executed if defined).
// Cleanup is ALWAYS attempted if CREATE executed (regardless of other failures).
func (e *LifecycleExecutor) Execute(ctx context.Context, lc *Lifecycle) *LifecycleResult {
	result := &LifecycleResult{
		Name:         lc.Name,
		CapturedVars: make(map[string]string),
	}

	// ===== STEP 1: CREATE =====
	createResult := e.executeStep(ctx, "create", lc.Create, nil)
	result.Steps = append(result.Steps, createResult)

	// Track whether CREATE actually executed (made HTTP request)
	createExecuted := createResult.StatusCode > 0 || createResult.Error != nil

	// Process CREATE result
	createSucceeded := false
	if createResult.Error != nil {
		result.CreateError = fmt.Errorf("request failed: %w", createResult.Error)
	} else if !isExpectedStatus(createResult.StatusCode, lc.Create.ExpectedStatus) {
		result.CreateError = fmt.Errorf("unexpected status %d (expected %v)",
			createResult.StatusCode, lc.Create.ExpectedStatus)
	} else {
		// Capture values from response
		captured, err := captureValues(createResult.Body, lc.Create.Capture)
		if err != nil {
			result.CreateError = fmt.Errorf("capture failed: %w", err)
		} else {
			// Store captured values
			createResult.Captured = captured
			for k, v := range captured {
				result.CapturedVars[k] = v
			}
			// Update step result with captured values
			result.Steps[0] = createResult
			createSucceeded = true
		}
	}

	// If CREATE didn't execute at all, skip everything
	if !createExecuted {
		result.CleanupSkipped = true
		return result
	}

	// ===== STEP 2: READ (optional) =====
	if lc.Read != nil {
		readResult := e.executeStep(ctx, "read", lc.Read, result.CapturedVars)
		result.Steps = append(result.Steps, readResult)

		// Process READ result
		if readResult.Error != nil {
			result.ReadError = fmt.Errorf("request failed: %w", readResult.Error)
		} else if !isExpectedStatus(readResult.StatusCode, lc.Read.ExpectedStatus) {
			result.ReadError = fmt.Errorf("unexpected status %d (expected %v)",
				readResult.StatusCode, lc.Read.ExpectedStatus)
		}
		// READ failures do NOT stop UPDATE or CLEANUP
	}

	// ===== STEP 3: UPDATE (optional) =====
	if lc.Update != nil {
		updateResult := e.executeStep(ctx, "update", lc.Update, result.CapturedVars)
		result.Steps = append(result.Steps, updateResult)

		// Process UPDATE result
		if updateResult.Error != nil {
			result.UpdateError = fmt.Errorf("request failed: %w", updateResult.Error)
		} else if !isExpectedStatus(updateResult.StatusCode, lc.Update.ExpectedStatus) {
			result.UpdateError = fmt.Errorf("unexpected status %d (expected %v)",
				updateResult.StatusCode, lc.Update.ExpectedStatus)
		}
		// UPDATE failures do NOT stop CLEANUP
	}

	// ===== STEP 4: CLEANUP (always attempted) =====
	// Skip only if we have no captured values AND CREATE failed
	// (if CREATE succeeded, we should try cleanup even without captured vars)
	if !createSucceeded && len(result.CapturedVars) == 0 {
		// CREATE failed and no captured values - cleanup path interpolation will fail
		// Still attempt cleanup with original path
	}

	cleanupResult := e.executeStep(ctx, "cleanup", lc.Cleanup, result.CapturedVars)
	result.Steps = append(result.Steps, cleanupResult)

	// Process CLEANUP result
	if cleanupResult.Error != nil {
		result.CleanupError = fmt.Errorf("request failed: %w", cleanupResult.Error)
	} else if !isExpectedStatus(cleanupResult.StatusCode, lc.Cleanup.ExpectedStatus) {
		result.CleanupError = fmt.Errorf("unexpected status %d (expected %v)",
			cleanupResult.StatusCode, lc.Cleanup.ExpectedStatus)
	}

	return result
}

// executeStep executes a single lifecycle step.
// captured is used for path interpolation (can be nil for CREATE step).
func (e *LifecycleExecutor) executeStep(ctx context.Context, stepName string, step *Step, captured map[string]string) StepResult {
	result := StepResult{
		StepName: stepName,
		Method:   strings.ToUpper(step.Method),
		Path:     step.Path,
	}

	// Interpolate path with captured values
	actualPath := step.Path
	if captured != nil && len(captured) > 0 {
		var err error
		actualPath, err = InterpolatePath(step.Path, captured)
		if err != nil {
			result.Error = fmt.Errorf("path interpolation failed: %w", err)
			return result
		}
	}
	result.ActualPath = actualPath

	// Build URL
	url := strings.TrimSuffix(e.config.BaseURL, "/") + actualPath

	// Build request body
	var bodyReader io.Reader
	if step.Body != nil {
		bodyBytes, err := json.Marshal(step.Body)
		if err != nil {
			result.Error = fmt.Errorf("failed to marshal request body: %w", err)
			return result
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, result.Method, url, bodyReader)
	if err != nil {
		result.Error = fmt.Errorf("failed to create request: %w", err)
		return result
	}

	// Set headers
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json, */*")
	req.Header.Set("User-Agent", "routecheck/1.0 (lifecycle)")

	// Custom headers from step
	for name, value := range step.Headers {
		req.Header.Set(name, value)
	}

	// Apply authentication
	if e.config.Auth != nil {
		e.config.Auth.Apply(req)
	}

	// Execute request
	start := time.Now()
	resp, err := e.client.Do(req)
	result.Duration = time.Since(start)

	if err != nil {
		result.Error = err
		return result
	}
	defer resp.Body.Close()

	// Read response body
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		result.Error = fmt.Errorf("failed to read response body: %w", err)
		return result
	}

	result.StatusCode = resp.StatusCode
	result.Body = string(bodyBytes)

	return result
}

// isExpectedStatus checks if the status code is in the expected list.
// If expected list is empty, any 2xx status is considered success.
func isExpectedStatus(statusCode int, expected []int) bool {
	if len(expected) == 0 {
		return statusCode >= 200 && statusCode < 300
	}
	for _, exp := range expected {
		if statusCode == exp {
			return true
		}
	}
	return false
}
