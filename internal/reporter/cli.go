package reporter

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/splenwilz/routecheck/internal/analyzer"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorGray   = "\033[90m"
	colorBold   = "\033[1m"
)

// CLIReporter outputs results in human-readable CLI format.
type CLIReporter struct {
	writer  io.Writer
	verbose bool
	noColor bool
}

// NewCLIReporter creates a new CLI reporter.
func NewCLIReporter(cfg Config) *CLIReporter {
	w := cfg.Writer
	if w == nil {
		w = os.Stdout
	}
	return &CLIReporter{
		writer:  w,
		verbose: cfg.Verbose,
		noColor: cfg.NoColor,
	}
}

// Report outputs analysis results in CLI format.
func (r *CLIReporter) Report(analyses []analyzer.Analysis, summary analyzer.Summary) error {
	// Print each result
	for _, a := range analyses {
		r.printAnalysis(a)
	}

	// Print summary
	fmt.Fprintln(r.writer)
	r.printSummary(summary)

	return nil
}

// printAnalysis prints a single analysis result.
func (r *CLIReporter) printAnalysis(a analyzer.Analysis) {
	// Status indicator
	var statusIcon, statusColor string
	switch a.Verdict {
	case analyzer.VerdictPass:
		statusIcon = "✓"
		statusColor = colorGreen
	case analyzer.VerdictFail:
		statusIcon = "✗"
		statusColor = colorRed
	case analyzer.VerdictSkip:
		statusIcon = "○"
		statusColor = colorGray
	case analyzer.VerdictWarning:
		statusIcon = "!"
		statusColor = colorYellow
	}

	// Method and path
	method := a.Result.Endpoint.Method
	path := a.Result.Endpoint.Path

	// Status line
	fmt.Fprintf(r.writer, "%s %s%-4s%s %s",
		r.color(statusIcon, statusColor),
		r.color("", colorBold),
		method,
		r.color("", colorReset),
		path,
	)

	// Response info
	if a.Result.Response.StatusCode > 0 {
		fmt.Fprintf(r.writer, " %s[%d]%s",
			r.color("", colorGray),
			a.Result.Response.StatusCode,
			r.color("", colorReset),
		)
	}

	// Duration
	if a.Result.Duration > 0 {
		fmt.Fprintf(r.writer, " %s(%s)%s",
			r.color("", colorGray),
			a.Result.Duration.Round(1e6), // Round to milliseconds
			r.color("", colorReset),
		)
	}

	fmt.Fprintln(r.writer)

	// Reason (for non-pass results or verbose mode)
	if a.Verdict != analyzer.VerdictPass || r.verbose {
		if a.Reason != "" {
			fmt.Fprintf(r.writer, "   %s%s%s\n",
				r.color("", colorGray),
				a.Reason,
				r.color("", colorReset),
			)
		}
	}

	// Verbose mode: show request/response details
	if r.verbose && a.Verdict != analyzer.VerdictSkip {
		r.printVerboseDetails(a)
	}

	// Suggestions for failures
	if a.Verdict == analyzer.VerdictFail && len(a.Suggestions) > 0 {
		fmt.Fprintf(r.writer, "   %sSuggestions:%s\n",
			r.color("", colorYellow),
			r.color("", colorReset),
		)
		for _, s := range a.Suggestions {
			fmt.Fprintf(r.writer, "   • %s\n", s)
		}
	}
}

// printVerboseDetails prints detailed request/response info.
func (r *CLIReporter) printVerboseDetails(a analyzer.Analysis) {
	// Request URL
	if a.Result.Request.URL != "" {
		fmt.Fprintf(r.writer, "   %sURL:%s %s\n",
			r.color("", colorBlue),
			r.color("", colorReset),
			a.Result.Request.URL,
		)
	}

	// Request headers
	if len(a.Result.Request.Headers) > 0 {
		fmt.Fprintf(r.writer, "   %sRequest Headers:%s\n",
			r.color("", colorBlue),
			r.color("", colorReset),
		)
		for k, v := range a.Result.Request.Headers {
			// Mask sensitive headers
			if strings.ToLower(k) == "authorization" {
				v = maskSensitive(v)
			}
			fmt.Fprintf(r.writer, "     %s: %s\n", k, v)
		}
	}

	// Response headers (selected)
	if len(a.Result.Response.Headers) > 0 {
		fmt.Fprintf(r.writer, "   %sResponse Headers:%s\n",
			r.color("", colorBlue),
			r.color("", colorReset),
		)
		for k, v := range a.Result.Response.Headers {
			fmt.Fprintf(r.writer, "     %s: %s\n", k, v)
		}
	}

	// Response body (truncated)
	if a.Result.Response.Body != "" {
		body := a.Result.Response.Body
		if len(body) > 500 {
			body = body[:500] + "..."
		}
		fmt.Fprintf(r.writer, "   %sResponse Body:%s\n     %s\n",
			r.color("", colorBlue),
			r.color("", colorReset),
			strings.ReplaceAll(body, "\n", "\n     "),
		)
	}
}

// printSummary prints the test summary.
func (r *CLIReporter) printSummary(summary analyzer.Summary) {
	fmt.Fprintf(r.writer, "%s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n",
		r.color("", colorGray),
		r.color("", colorReset),
	)

	// Overall result
	var resultText, resultColor string
	if summary.Failed > 0 {
		resultText = "FAILED"
		resultColor = colorRed
	} else if summary.Warnings > 0 {
		resultText = "PASSED WITH WARNINGS"
		resultColor = colorYellow
	} else if summary.Passed > 0 {
		resultText = "PASSED"
		resultColor = colorGreen
	} else {
		resultText = "NO TESTS RUN"
		resultColor = colorGray
	}

	fmt.Fprintf(r.writer, "%s%s%s\n",
		r.color(resultText, resultColor+colorBold),
		r.color("", colorReset),
		"",
	)

	// Stats
	fmt.Fprintf(r.writer, "Total: %d | ", summary.Total)
	fmt.Fprintf(r.writer, "%sPassed: %d%s | ",
		r.color("", colorGreen),
		summary.Passed,
		r.color("", colorReset),
	)
	fmt.Fprintf(r.writer, "%sFailed: %d%s",
		r.color("", colorRed),
		summary.Failed,
		r.color("", colorReset),
	)

	if summary.Warnings > 0 {
		fmt.Fprintf(r.writer, " | %sWarnings: %d%s",
			r.color("", colorYellow),
			summary.Warnings,
			r.color("", colorReset),
		)
	}

	if summary.Skipped > 0 {
		fmt.Fprintf(r.writer, " | %sSkipped: %d%s",
			r.color("", colorGray),
			summary.Skipped,
			r.color("", colorReset),
		)
	}

	fmt.Fprintln(r.writer)
}

// color wraps text with ANSI color codes if colors are enabled.
func (r *CLIReporter) color(text, code string) string {
	if r.noColor {
		return text
	}
	if text == "" {
		return code
	}
	return code + text + colorReset
}

// maskSensitive masks sensitive values like tokens.
func maskSensitive(value string) string {
	if len(value) <= 10 {
		return "***"
	}
	return value[:6] + "***" + value[len(value)-4:]
}
