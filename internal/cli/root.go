// Package cli implements the command-line interface for routecheck.
// This layer handles input/output only - no business logic.
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/splenwilz/routecheck/internal/analyzer"
	"github.com/splenwilz/routecheck/internal/discovery"
	"github.com/splenwilz/routecheck/internal/executor"
	"github.com/splenwilz/routecheck/internal/normalize"
	"github.com/splenwilz/routecheck/internal/reporter"
	"github.com/splenwilz/routecheck/pkg/auth"
	"github.com/splenwilz/routecheck/pkg/endpoint"
)

const (
	exitOK    = 0
	exitError = 1
)

var version = "dev"

// Run executes the CLI with the given arguments and returns an exit code.
func Run(args []string) int {
	if len(args) == 0 {
		printUsage(os.Stdout)
		return exitOK
	}

	switch args[0] {
	case "probe":
		return runProbe(args[1:])
	case "discover":
		return runDiscover(args[1:])
	case "validate":
		return runValidate(args[1:])
	case "version", "--version", "-v":
		fmt.Printf("routecheck %s\n", version)
		return exitOK
	case "help", "--help", "-h":
		printUsage(os.Stdout)
		return exitOK
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", args[0])
		printUsage(os.Stderr)
		return exitError
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `routecheck - Runtime REST API testing tool

Usage:
  routecheck <command> [options]

Commands:
  discover  List API endpoints without executing requests
  probe     Discover and test API endpoints (safe methods only)
  validate  Test all endpoints (use --unsafe for mutations)
  version   Print version information
  help      Show this help message

Examples:
  routecheck discover https://api.example.com
  routecheck probe https://api.example.com
  routecheck validate --unsafe https://api.example.com

Use "routecheck <command> --help" for more information about a command.`)
}

// runProbe implements the probe command.
// It discovers endpoints and tests them using safe HTTP methods only.
func runProbe(args []string) int {
	fs := flag.NewFlagSet("probe", flag.ContinueOnError)

	// Discovery options
	openAPIPath := fs.String("openapi", "", "Path or URL to OpenAPI/Swagger spec")
	mapPath := fs.String("map", "", "Path to manual endpoint map file (YAML or JSON)")

	// Execution options
	timeout := fs.Int("timeout", 30, "Request timeout in seconds")
	concurrency := fs.Int("concurrency", 5, "Maximum concurrent requests")

	// Authentication options
	authFlag := fs.String("auth", "", "Authentication: bearer:<token>, api_key:<key>, or basic:<user>:<pass>")
	authHeader := fs.String("auth-header", "", "Custom header name for API key auth (default: X-API-Key)")

	// Output options
	outputFormat := fs.String("output", "cli", "Output format: cli, json")
	verbose := fs.Bool("verbose", false, "Enable verbose output")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, `Usage: routecheck probe [options] <base-url>

Discover and test API endpoints using safe HTTP methods (GET, HEAD, OPTIONS).

Options:`)
		fs.PrintDefaults()
		fmt.Fprintln(os.Stderr, `
Examples:
  routecheck probe https://api.example.com
  routecheck probe --openapi ./spec.yaml https://api.example.com
  routecheck probe --map ./endpoints.yaml https://api.example.com
  routecheck probe --auth bearer:token https://api.example.com`)
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return exitOK
		}
		return exitError
	}

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "error: base-url is required")
		fs.Usage()
		return exitError
	}

	baseURL := fs.Arg(0)

	// Parse authentication
	var authProvider auth.Provider
	if *authFlag != "" {
		var err error
		authProvider, err = parseAuth(*authFlag, *authHeader)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return exitError
		}
	}

	// Create probe configuration
	cfg := ProbeConfig{
		BaseURL:     baseURL,
		OpenAPIPath: *openAPIPath,
		ManualPath:  *mapPath,
		Timeout:     *timeout,
		Concurrency: *concurrency,
		Output:      *outputFormat,
		Verbose:     *verbose,
		Auth:        authProvider,
	}

	// Execute probe
	return executeProbe(cfg)
}

// ProbeConfig holds configuration for the probe command.
type ProbeConfig struct {
	BaseURL     string
	OpenAPIPath string
	ManualPath  string
	Timeout     int
	Concurrency int
	Output      string
	Verbose     bool
	Auth        auth.Provider
}

// executeProbe runs the probe pipeline.
// Pipeline: discovery → normalize → execute → analyze → report
func executeProbe(cfg ProbeConfig) int {
	ctx := context.Background()

	// Layer 2: Discovery
	fmt.Fprintf(os.Stderr, "Discovering endpoints from %s...\n", cfg.BaseURL)

	discoveryCfg := discovery.Config{
		BaseURL:     cfg.BaseURL,
		OpenAPIPath: cfg.OpenAPIPath,
		ManualPath:  cfg.ManualPath,
		AutoProbe:   true,
	}

	rawEndpoints, sourceName, err := discovery.Discover(ctx, discoveryCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitError
	}

	fmt.Fprintf(os.Stderr, "Found %d endpoints via %s\n", len(rawEndpoints), sourceName)

	// Layer 3: Normalize
	endpoints := normalize.Normalize(rawEndpoints)

	// Filter to safe methods only for probe command
	safeEndpoints := filterSafe(endpoints)
	if len(safeEndpoints) == 0 {
		fmt.Fprintln(os.Stderr, "No safe endpoints (GET/HEAD/OPTIONS) found to probe")
		fmt.Fprintln(os.Stderr, "Use 'routecheck validate' to test all endpoints including unsafe methods")
		return exitOK
	}

	fmt.Fprintf(os.Stderr, "Testing %d safe endpoints...\n\n", len(safeEndpoints))

	// Layer 5: Execute (Layer 4 param gen is called by executor)
	exec := executor.New(executor.Config{
		BaseURL:     cfg.BaseURL,
		Timeout:     time.Duration(cfg.Timeout) * time.Second,
		Concurrency: cfg.Concurrency,
		SafeOnly:    true,
		Auth:        cfg.Auth,
		MaxRetries:  1,
		RetryDelay:  500 * time.Millisecond,
	})

	results := exec.Execute(ctx, safeEndpoints)

	// Layer 6: Analyze
	analyses, summary := analyzer.AnalyzeAll(results)

	// Layer 7: Report
	rep := reporter.New(cfg.Output, reporter.Config{
		Writer:  os.Stdout,
		Verbose: cfg.Verbose,
	})

	if err := rep.Report(analyses, summary); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing report: %v\n", err)
		return exitError
	}

	// Exit code based on results
	if summary.Failed > 0 {
		return exitError
	}
	return exitOK
}

// filterSafe returns only endpoints with safe HTTP methods.
func filterSafe(endpoints []endpoint.Endpoint) []endpoint.Endpoint {
	var safe []endpoint.Endpoint
	for _, ep := range endpoints {
		if ep.IsSafe() {
			safe = append(safe, ep)
		}
	}
	return safe
}

// runDiscover implements the discover command.
// It lists endpoints without executing any requests.
func runDiscover(args []string) int {
	fs := flag.NewFlagSet("discover", flag.ContinueOnError)

	// Discovery options
	openAPIPath := fs.String("openapi", "", "Path or URL to OpenAPI/Swagger spec")
	mapPath := fs.String("map", "", "Path to manual endpoint map file (YAML or JSON)")

	// Output options
	outputFormat := fs.String("output", "cli", "Output format: cli, json")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, `Usage: routecheck discover [options] <base-url>

List API endpoints without executing any requests. Read-only operation.

Options:`)
		fs.PrintDefaults()
		fmt.Fprintln(os.Stderr, `
Examples:
  routecheck discover https://api.example.com
  routecheck discover --openapi ./spec.yaml https://api.example.com
  routecheck discover --map ./endpoints.yaml https://api.example.com`)
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return exitOK
		}
		return exitError
	}

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "error: base-url is required")
		fs.Usage()
		return exitError
	}

	baseURL := fs.Arg(0)

	// Layer 2: Discovery
	ctx := context.Background()

	discoveryCfg := discovery.Config{
		BaseURL:     baseURL,
		OpenAPIPath: *openAPIPath,
		ManualPath:  *mapPath,
		AutoProbe:   true,
	}

	rawEndpoints, sourceName, err := discovery.Discover(ctx, discoveryCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitError
	}

	// Layer 3: Normalize
	endpoints := normalize.Normalize(rawEndpoints)

	// Output results
	if *outputFormat == "json" {
		return outputDiscoverJSON(endpoints, sourceName)
	}
	return outputDiscoverCLI(endpoints, sourceName)
}

// runValidate implements the validate command.
// It tests all endpoints, requiring --unsafe for mutation methods.
func runValidate(args []string) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)

	// Discovery options
	openAPIPath := fs.String("openapi", "", "Path or URL to OpenAPI/Swagger spec")
	mapPath := fs.String("map", "", "Path to manual endpoint map file (YAML or JSON)")

	// Execution options
	timeout := fs.Int("timeout", 30, "Request timeout in seconds")
	concurrency := fs.Int("concurrency", 5, "Maximum concurrent requests")
	unsafe := fs.Bool("unsafe", false, "Enable testing of unsafe methods (POST, PUT, PATCH, DELETE)")

	// Authentication options
	authFlag := fs.String("auth", "", "Authentication: bearer:<token>, api_key:<key>, or basic:<user>:<pass>")
	authHeader := fs.String("auth-header", "", "Custom header name for API key auth (default: X-API-Key)")

	// Output options
	outputFormat := fs.String("output", "cli", "Output format: cli, json")
	verbose := fs.Bool("verbose", false, "Enable verbose output")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, `Usage: routecheck validate [options] <base-url>

Test API endpoints. Safe methods (GET, HEAD, OPTIONS) run by default.
Use --unsafe to include mutation methods (POST, PUT, PATCH, DELETE).

Options:`)
		fs.PrintDefaults()
		fmt.Fprintln(os.Stderr, `
Examples:
  routecheck validate https://api.example.com
  routecheck validate --unsafe https://api.example.com
  routecheck validate --map ./endpoints.yaml --unsafe https://api.example.com
  routecheck validate --auth bearer:token https://api.example.com`)
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return exitOK
		}
		return exitError
	}

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "error: base-url is required")
		fs.Usage()
		return exitError
	}

	baseURL := fs.Arg(0)

	// Parse authentication
	var authProvider auth.Provider
	if *authFlag != "" {
		var err error
		authProvider, err = parseAuth(*authFlag, *authHeader)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return exitError
		}
	}

	// Create validate configuration
	cfg := ValidateConfig{
		BaseURL:     baseURL,
		OpenAPIPath: *openAPIPath,
		ManualPath:  *mapPath,
		Timeout:     *timeout,
		Concurrency: *concurrency,
		Unsafe:      *unsafe,
		Output:      *outputFormat,
		Verbose:     *verbose,
		Auth:        authProvider,
	}

	return executeValidate(cfg)
}

// ValidateConfig holds configuration for the validate command.
type ValidateConfig struct {
	BaseURL     string
	OpenAPIPath string
	ManualPath  string
	Timeout     int
	Concurrency int
	Unsafe      bool
	Output      string
	Verbose     bool
	Auth        auth.Provider
}

// executeValidate runs the validate pipeline.
func executeValidate(cfg ValidateConfig) int {
	ctx := context.Background()

	// Layer 2: Discovery
	fmt.Fprintf(os.Stderr, "Discovering endpoints from %s...\n", cfg.BaseURL)

	discoveryCfg := discovery.Config{
		BaseURL:     cfg.BaseURL,
		OpenAPIPath: cfg.OpenAPIPath,
		ManualPath:  cfg.ManualPath,
		AutoProbe:   true,
	}

	rawEndpoints, sourceName, err := discovery.Discover(ctx, discoveryCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitError
	}

	fmt.Fprintf(os.Stderr, "Found %d endpoints via %s\n", len(rawEndpoints), sourceName)

	// Layer 3: Normalize
	endpoints := normalize.Normalize(rawEndpoints)

	// Filter endpoints based on --unsafe flag
	var testEndpoints []endpoint.Endpoint
	var skippedCount int

	if cfg.Unsafe {
		// Warn user about unsafe operations
		unsafeCount := 0
		for _, ep := range endpoints {
			if !ep.IsSafe() {
				unsafeCount++
			}
		}
		if unsafeCount > 0 {
			fmt.Fprintf(os.Stderr, "\n⚠️  WARNING: --unsafe enabled. %d mutation endpoints will be tested.\n", unsafeCount)
			fmt.Fprintf(os.Stderr, "   This may modify server state. Press Ctrl+C within 3 seconds to abort.\n\n")
			time.Sleep(3 * time.Second)
		}
		testEndpoints = endpoints
	} else {
		// Safe only - filter and count skipped
		for _, ep := range endpoints {
			if ep.IsSafe() {
				testEndpoints = append(testEndpoints, ep)
			} else {
				skippedCount++
			}
		}
		if skippedCount > 0 {
			fmt.Fprintf(os.Stderr, "Skipping %d unsafe endpoints (use --unsafe to include)\n", skippedCount)
		}
	}

	if len(testEndpoints) == 0 {
		fmt.Fprintln(os.Stderr, "No endpoints to test")
		return exitOK
	}

	fmt.Fprintf(os.Stderr, "Testing %d endpoints...\n\n", len(testEndpoints))

	// Layer 5: Execute
	exec := executor.New(executor.Config{
		BaseURL:     cfg.BaseURL,
		Timeout:     time.Duration(cfg.Timeout) * time.Second,
		Concurrency: cfg.Concurrency,
		SafeOnly:    !cfg.Unsafe,
		Auth:        cfg.Auth,
		MaxRetries:  1,
		RetryDelay:  500 * time.Millisecond,
	})

	results := exec.Execute(ctx, testEndpoints)

	// Layer 6: Analyze
	analyses, summary := analyzer.AnalyzeAll(results)

	// Layer 7: Report
	rep := reporter.New(cfg.Output, reporter.Config{
		Writer:  os.Stdout,
		Verbose: cfg.Verbose,
	})

	if err := rep.Report(analyses, summary); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing report: %v\n", err)
		return exitError
	}

	// Exit code based on results
	if summary.Failed > 0 {
		return exitError
	}
	return exitOK
}

// outputDiscoverCLI outputs discovered endpoints in CLI format.
func outputDiscoverCLI(endpoints []endpoint.Endpoint, source string) int {
	fmt.Printf("Discovered %d endpoints via %s\n\n", len(endpoints), source)

	// Group by path for cleaner output
	for _, ep := range endpoints {
		methodColor := methodColorCode(ep.Method)
		safeIndicator := ""
		if !ep.IsSafe() {
			safeIndicator = " [unsafe]"
		}

		fmt.Printf("%s%-7s%s %s%s\n", methodColor, ep.Method, colorReset, ep.Path, safeIndicator)

		// Show parameters if any
		if len(ep.PathParams) > 0 {
			fmt.Printf("        Path params: %s\n", formatParams(ep.PathParams))
		}
		if len(ep.QueryParams) > 0 {
			fmt.Printf("        Query params: %s\n", formatParams(ep.QueryParams))
		}
		if ep.Auth != nil {
			fmt.Printf("        Auth: %s\n", ep.Auth.Type)
		}
	}

	// Summary
	safeCount := 0
	unsafeCount := 0
	for _, ep := range endpoints {
		if ep.IsSafe() {
			safeCount++
		} else {
			unsafeCount++
		}
	}

	fmt.Printf("\nSummary: %d safe, %d unsafe\n", safeCount, unsafeCount)
	return exitOK
}

// outputDiscoverJSON outputs discovered endpoints in JSON format.
func outputDiscoverJSON(endpoints []endpoint.Endpoint, source string) int {
	type jsonEndpoint struct {
		Method      string   `json:"method"`
		Path        string   `json:"path"`
		Safe        bool     `json:"safe"`
		PathParams  []string `json:"pathParams,omitempty"`
		QueryParams []string `json:"queryParams,omitempty"`
		AuthType    string   `json:"authType,omitempty"`
	}

	type jsonOutput struct {
		Source    string         `json:"source"`
		Total     int            `json:"total"`
		Safe      int            `json:"safe"`
		Unsafe    int            `json:"unsafe"`
		Endpoints []jsonEndpoint `json:"endpoints"`
	}

	output := jsonOutput{
		Source:    source,
		Total:     len(endpoints),
		Endpoints: make([]jsonEndpoint, 0, len(endpoints)),
	}

	for _, ep := range endpoints {
		je := jsonEndpoint{
			Method: ep.Method,
			Path:   ep.Path,
			Safe:   ep.IsSafe(),
		}

		if ep.IsSafe() {
			output.Safe++
		} else {
			output.Unsafe++
		}

		for _, p := range ep.PathParams {
			je.PathParams = append(je.PathParams, p.Name)
		}
		for _, p := range ep.QueryParams {
			je.QueryParams = append(je.QueryParams, p.Name)
		}
		if ep.Auth != nil {
			je.AuthType = ep.Auth.Type
		}

		output.Endpoints = append(output.Endpoints, je)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(output); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
		return exitError
	}

	return exitOK
}

// methodColorCode returns ANSI color code for HTTP methods.
func methodColorCode(method string) string {
	switch method {
	case "GET":
		return "\033[32m" // green
	case "POST":
		return "\033[33m" // yellow
	case "PUT":
		return "\033[34m" // blue
	case "PATCH":
		return "\033[36m" // cyan
	case "DELETE":
		return "\033[31m" // red
	default:
		return "\033[37m" // white
	}
}

const colorReset = "\033[0m"

// formatParams formats parameters for display.
func formatParams(params []endpoint.Param) string {
	names := make([]string, 0, len(params))
	for _, p := range params {
		name := p.Name
		if p.Required {
			name += "*"
		}
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

// parseAuth parses the --auth flag and returns the appropriate provider.
// Format: type:value where type is bearer, api_key, or basic.
// For basic auth: basic:username:password
func parseAuth(authStr, headerName string) (auth.Provider, error) {
	// Find the first colon to split type from value
	idx := strings.Index(authStr, ":")
	if idx == -1 {
		return nil, fmt.Errorf("invalid auth format: expected type:value (e.g., bearer:token)")
	}

	authType := strings.ToLower(authStr[:idx])
	value := authStr[idx+1:]

	if value == "" {
		return nil, fmt.Errorf("invalid auth format: value cannot be empty")
	}

	switch authType {
	case "bearer":
		return auth.NewBearerProvider(value), nil

	case "api_key":
		return auth.NewAPIKeyProvider(value, headerName), nil

	case "basic":
		// For basic auth, value is username:password
		colonIdx := strings.Index(value, ":")
		if colonIdx == -1 {
			return nil, fmt.Errorf("invalid basic auth format: expected basic:username:password")
		}
		username := value[:colonIdx]
		password := value[colonIdx+1:]
		if username == "" {
			return nil, fmt.Errorf("invalid basic auth: username cannot be empty")
		}
		return auth.NewBasicProvider(username, password), nil

	default:
		return nil, fmt.Errorf("unknown auth type: %s (supported: bearer, api_key, basic)", authType)
	}
}
