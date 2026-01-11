// Package analyzer interprets execution results into verdicts.
// This is Layer 6 of the pipeline - it determines pass/fail for each endpoint.
package analyzer

import (
	"fmt"
	"strings"

	"github.com/splenwilz/routecheck/internal/executor"
)

// Verdict represents the outcome of analyzing an endpoint result.
type Verdict string

const (
	VerdictPass    Verdict = "PASS"
	VerdictFail    Verdict = "FAIL"
	VerdictSkip    Verdict = "SKIP"
	VerdictWarning Verdict = "WARN"
)

// Analysis represents the analyzed result of an endpoint test.
type Analysis struct {
	Result      executor.Result
	Verdict     Verdict
	Reason      string   // Human-readable explanation
	Suggestions []string // Suggestions for fixing issues
}

// Summary holds aggregate statistics for all analyzed endpoints.
type Summary struct {
	Total    int
	Passed   int
	Failed   int
	Skipped  int
	Warnings int
}

// Analyze interprets an execution result and returns an analysis.
func Analyze(result executor.Result) Analysis {
	analysis := Analysis{Result: result}

	// Handle skipped endpoints
	if result.SkipReason != "" {
		analysis.Verdict = VerdictSkip
		analysis.Reason = result.SkipReason
		return analysis
	}

	// Handle connection/request errors
	if result.Error != nil {
		analysis.Verdict = VerdictFail
		analysis.Reason = fmt.Sprintf("Request failed: %s", result.Error.Error())
		analysis.Suggestions = suggestionsForError(result.Error)
		return analysis
	}

	// Analyze based on status code
	statusCode := result.Response.StatusCode
	endpoint := result.Endpoint

	// Check if status code is expected success
	if endpoint.IsSuccess(statusCode) {
		analysis.Verdict = VerdictPass
		analysis.Reason = fmt.Sprintf("Responded with %d", statusCode)
		return analysis
	}

	// Server errors (5xx) are always failures
	if statusCode >= 500 && statusCode < 600 {
		analysis.Verdict = VerdictFail
		analysis.Reason = fmt.Sprintf("Server error: %s", result.Response.Status)
		analysis.Suggestions = []string{
			"Check server logs for error details",
			"Verify the service is healthy",
			"Check for database or dependency issues",
		}
		return analysis
	}

	// Client errors (4xx) need more nuanced handling
	if statusCode >= 400 && statusCode < 500 {
		return analyzeClientError(result)
	}

	// Redirects (3xx)
	if statusCode >= 300 && statusCode < 400 {
		analysis.Verdict = VerdictWarning
		analysis.Reason = fmt.Sprintf("Redirect: %s", result.Response.Status)
		if location := result.Response.Headers["Location"]; location != "" {
			analysis.Reason += fmt.Sprintf(" -> %s", location)
		}
		return analysis
	}

	// Unexpected status codes
	analysis.Verdict = VerdictWarning
	analysis.Reason = fmt.Sprintf("Unexpected status: %s", result.Response.Status)
	return analysis
}

// analyzeClientError provides detailed analysis for 4xx errors.
func analyzeClientError(result executor.Result) Analysis {
	analysis := Analysis{Result: result}
	statusCode := result.Response.StatusCode

	switch statusCode {
	case 400:
		// Bad Request - could be our parameter generation
		analysis.Verdict = VerdictFail
		analysis.Reason = "Bad Request - invalid parameters sent"
		analysis.Suggestions = []string{
			"Check if required parameters are being sent",
			"Verify parameter types and formats",
			"Review the API documentation for this endpoint",
		}

	case 401:
		// Unauthorized - missing or invalid auth
		analysis.Verdict = VerdictFail
		analysis.Reason = "Unauthorized - authentication required or invalid"
		analysis.Suggestions = []string{
			"Provide authentication credentials with --auth flag",
			"Check if the token/API key is valid and not expired",
		}

	case 403:
		// Forbidden - auth works but insufficient permissions
		analysis.Verdict = VerdictFail
		analysis.Reason = "Forbidden - insufficient permissions"
		analysis.Suggestions = []string{
			"Check if the authenticated user has access to this resource",
			"Verify API key permissions/scopes",
		}

	case 404:
		// Not Found - endpoint might not exist or resource missing
		analysis.Verdict = VerdictFail
		analysis.Reason = "Not Found - endpoint or resource does not exist"
		analysis.Suggestions = []string{
			"Verify the endpoint path is correct",
			"Check if path parameters are valid",
			"Confirm the resource exists",
		}

	case 405:
		// Method Not Allowed
		analysis.Verdict = VerdictFail
		analysis.Reason = fmt.Sprintf("Method %s not allowed", result.Endpoint.Method)
		analysis.Suggestions = []string{
			"Check the API documentation for allowed methods",
			"This endpoint may not support the tested HTTP method",
		}

	case 422:
		// Unprocessable Entity - validation error
		analysis.Verdict = VerdictFail
		analysis.Reason = "Unprocessable Entity - validation failed"
		analysis.Suggestions = []string{
			"Check the response body for validation error details",
			"Verify request body matches expected schema",
		}

	case 429:
		// Too Many Requests - rate limited
		analysis.Verdict = VerdictWarning
		analysis.Reason = "Rate limited - too many requests"
		analysis.Suggestions = []string{
			"Reduce request concurrency with --concurrency flag",
			"Wait before retrying",
		}

	default:
		analysis.Verdict = VerdictFail
		analysis.Reason = fmt.Sprintf("Client error: %s", result.Response.Status)
	}

	// Try to extract error message from response body
	if errMsg := extractErrorMessage(result.Response.Body); errMsg != "" {
		analysis.Reason += fmt.Sprintf(" - %s", errMsg)
	}

	return analysis
}

// extractErrorMessage tries to extract an error message from the response body.
func extractErrorMessage(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}

	// Truncate long messages
	if len(body) > 200 {
		return body[:200] + "..."
	}

	return body
}

// suggestionsForError provides suggestions based on the error type.
func suggestionsForError(err error) []string {
	errStr := err.Error()

	if strings.Contains(errStr, "connection refused") {
		return []string{
			"Verify the server is running",
			"Check if the base URL is correct",
			"Verify network connectivity",
		}
	}

	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline exceeded") {
		return []string{
			"Increase timeout with --timeout flag",
			"Check if the server is under heavy load",
			"Verify network latency",
		}
	}

	if strings.Contains(errStr, "no such host") || strings.Contains(errStr, "DNS") {
		return []string{
			"Verify the hostname is correct",
			"Check DNS resolution",
		}
	}

	if strings.Contains(errStr, "certificate") || strings.Contains(errStr, "TLS") {
		return []string{
			"Check SSL/TLS certificate validity",
			"Verify the server's certificate is trusted",
		}
	}

	return []string{
		"Check network connectivity",
		"Verify the base URL is correct",
	}
}

// AnalyzeAll analyzes all results and returns analyses plus a summary.
func AnalyzeAll(results []executor.Result) ([]Analysis, Summary) {
	analyses := make([]Analysis, len(results))
	var summary Summary

	for i, result := range results {
		analyses[i] = Analyze(result)
		summary.Total++

		switch analyses[i].Verdict {
		case VerdictPass:
			summary.Passed++
		case VerdictFail:
			summary.Failed++
		case VerdictSkip:
			summary.Skipped++
		case VerdictWarning:
			summary.Warnings++
		}
	}

	return analyses, summary
}
