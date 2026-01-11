// Package reporter formats and outputs test results.
// This is Layer 7 of the pipeline - it produces human or machine readable output.
package reporter

import (
	"io"

	"github.com/splenwilz/routecheck/internal/analyzer"
)

// Reporter is the interface for outputting test results.
type Reporter interface {
	// Report outputs the analysis results.
	Report(analyses []analyzer.Analysis, summary analyzer.Summary) error
}

// Config holds reporter configuration.
type Config struct {
	Writer  io.Writer
	Verbose bool
	NoColor bool
}

// New creates a reporter based on the format name.
func New(format string, cfg Config) Reporter {
	switch format {
	case "json":
		return NewJSONReporter(cfg)
	default:
		return NewCLIReporter(cfg)
	}
}
