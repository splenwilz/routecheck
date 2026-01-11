package reporter

import (
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/splenwilz/routecheck/internal/analyzer"
)

// JSONReporter outputs results in JSON format for CI integration.
type JSONReporter struct {
	writer  io.Writer
	verbose bool
}

// NewJSONReporter creates a new JSON reporter.
func NewJSONReporter(cfg Config) *JSONReporter {
	w := cfg.Writer
	if w == nil {
		w = os.Stdout
	}
	return &JSONReporter{
		writer:  w,
		verbose: cfg.Verbose,
	}
}

// JSONOutput is the top-level JSON output structure.
type JSONOutput struct {
	Timestamp string        `json:"timestamp"`
	Summary   JSONSummary   `json:"summary"`
	Results   []JSONResult  `json:"results"`
}

// JSONSummary is the summary in JSON format.
type JSONSummary struct {
	Total    int  `json:"total"`
	Passed   int  `json:"passed"`
	Failed   int  `json:"failed"`
	Skipped  int  `json:"skipped"`
	Warnings int  `json:"warnings"`
	Success  bool `json:"success"`
}

// JSONResult is a single test result in JSON format.
type JSONResult struct {
	Method      string            `json:"method"`
	Path        string            `json:"path"`
	Verdict     string            `json:"verdict"`
	Reason      string            `json:"reason,omitempty"`
	Suggestions []string          `json:"suggestions,omitempty"`
	Request     *JSONRequest      `json:"request,omitempty"`
	Response    *JSONResponse     `json:"response,omitempty"`
	Duration    string            `json:"duration,omitempty"`
	Retries     int               `json:"retries,omitempty"`
}

// JSONRequest is request details in JSON format.
type JSONRequest struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers,omitempty"`
}

// JSONResponse is response details in JSON format.
type JSONResponse struct {
	StatusCode int               `json:"statusCode"`
	Status     string            `json:"status"`
	Headers    map[string]string `json:"headers,omitempty"`
	BodySize   int               `json:"bodySize"`
}

// Report outputs analysis results in JSON format.
func (r *JSONReporter) Report(analyses []analyzer.Analysis, summary analyzer.Summary) error {
	output := JSONOutput{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Summary: JSONSummary{
			Total:    summary.Total,
			Passed:   summary.Passed,
			Failed:   summary.Failed,
			Skipped:  summary.Skipped,
			Warnings: summary.Warnings,
			Success:  summary.Failed == 0,
		},
		Results: make([]JSONResult, 0, len(analyses)),
	}

	for _, a := range analyses {
		result := JSONResult{
			Method:      a.Result.Endpoint.Method,
			Path:        a.Result.Endpoint.Path,
			Verdict:     string(a.Verdict),
			Reason:      a.Reason,
			Suggestions: a.Suggestions,
		}

		if a.Result.Duration > 0 {
			result.Duration = a.Result.Duration.String()
		}

		if a.Result.Retries > 0 {
			result.Retries = a.Result.Retries
		}

		// Include request/response details in verbose mode or for failures
		if r.verbose || a.Verdict == analyzer.VerdictFail {
			if a.Result.Request.URL != "" {
				result.Request = &JSONRequest{
					URL:     a.Result.Request.URL,
					Method:  a.Result.Request.Method,
					Headers: sanitizeHeaders(a.Result.Request.Headers),
				}
			}

			if a.Result.Response.StatusCode > 0 {
				result.Response = &JSONResponse{
					StatusCode: a.Result.Response.StatusCode,
					Status:     a.Result.Response.Status,
					Headers:    a.Result.Response.Headers,
					BodySize:   a.Result.Response.BodySize,
				}
			}
		}

		output.Results = append(output.Results, result)
	}

	encoder := json.NewEncoder(r.writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

// sanitizeHeaders removes or masks sensitive header values.
func sanitizeHeaders(headers map[string]string) map[string]string {
	result := make(map[string]string, len(headers))
	for k, v := range headers {
		switch k {
		case "Authorization":
			result[k] = "[REDACTED]"
		default:
			result[k] = v
		}
	}
	return result
}
