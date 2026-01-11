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
	"github.com/splenwilz/routecheck/internal/lifecycle"
	"github.com/splenwilz/routecheck/internal/normalize"
	"github.com/splenwilz/routecheck/internal/reporter"
	"github.com/splenwilz/routecheck/pkg/auth"
	"github.com/splenwilz/routecheck/pkg/endpoint"
)

const (
	exitOK             = 0
	exitError          = 1
	exitCleanupFailure = 2 // Lifecycle cleanup failed - orphaned resources may exist
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

	// Lifecycle options (v2)
	enableLifecycle := fs.Bool("enable-lifecycle", false, "Enable lifecycle testing (create/read/update/delete sequences)")
	ackMutations := fs.Bool("ack-mutations", false, "Acknowledge that lifecycle testing will create resources and cleanup may fail")
	lifecycleFile := fs.String("lifecycle-file", "", "Path to lifecycle spec file (required with --enable-lifecycle)")

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

Lifecycle Testing (v2):
  Use --enable-lifecycle with --ack-mutations and --lifecycle-file for CRUD sequences.
  WARNING: Lifecycle testing creates real resources. Cleanup may fail.

Options:`)
		fs.PrintDefaults()
		fmt.Fprintln(os.Stderr, `
Examples:
  routecheck validate https://api.example.com
  routecheck validate --unsafe https://api.example.com
  routecheck validate --map ./endpoints.yaml --unsafe https://api.example.com
  routecheck validate --auth bearer:token https://api.example.com

Lifecycle Examples:
  routecheck validate --enable-lifecycle --ack-mutations --lifecycle-file ./lifecycles.yaml https://api.example.com`)
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

	// Validate lifecycle flags (Phase 0: validation only, no execution)
	if *enableLifecycle {
		// Require acknowledgement flag
		if !*ackMutations {
			fmt.Fprintln(os.Stderr, `error: --enable-lifecycle requires --ack-mutations flag

Lifecycle testing creates real resources and cleanup may fail.
Add --ack-mutations to acknowledge this risk.`)
			return exitError
		}

		// Require lifecycle file
		if *lifecycleFile == "" {
			fmt.Fprintln(os.Stderr, `error: --enable-lifecycle requires --lifecycle-file

Specify a lifecycle spec file with --lifecycle-file <path>`)
			return exitError
		}

		// Refuse invalid flag combinations
		if *openAPIPath != "" || *mapPath != "" {
			fmt.Fprintln(os.Stderr, `error: --enable-lifecycle cannot be combined with --openapi or --map

Lifecycle mode uses its own spec file (--lifecycle-file).
Standard endpoint discovery is not used in lifecycle mode.`)
			return exitError
		}

		// Warn about redundant flags
		if *unsafe {
			fmt.Fprintln(os.Stderr, "note: --unsafe is redundant with --enable-lifecycle (lifecycle mode always performs mutations)")
		}

		// Load and validate lifecycle spec
		spec, err := lifecycle.LoadSpec(*lifecycleFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return exitError
		}

		fmt.Fprintf(os.Stderr, "Loaded %d lifecycle(s) from %s\n", len(spec.Lifecycles), *lifecycleFile)
		fmt.Fprintln(os.Stderr, "")

		// Execute lifecycle testing
		return executeLifecycleCreate(spec, baseURL, time.Duration(*timeout)*time.Second, authProvider)
	}

	// Warn if lifecycle flags provided without --enable-lifecycle
	if *ackMutations && !*enableLifecycle {
		fmt.Fprintln(os.Stderr, "warning: --ack-mutations has no effect without --enable-lifecycle")
	}
	if *lifecycleFile != "" && !*enableLifecycle {
		fmt.Fprintln(os.Stderr, "warning: --lifecycle-file has no effect without --enable-lifecycle")
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

// executeLifecycleCreate executes full lifecycle: CREATE → READ → UPDATE → CLEANUP.
// Phase 3: All steps supported. READ and UPDATE are optional.
//
// Exit codes:
//   - 0: All lifecycles passed
//   - 1: One or more lifecycle steps failed (CREATE, READ, UPDATE expected status)
//   - 2: Cleanup failed - orphaned resources may exist (requires manual cleanup)
func executeLifecycleCreate(spec *lifecycle.Spec, baseURL string, timeout time.Duration, authProvider auth.Provider) int {
	ctx := context.Background()

	exec := lifecycle.NewExecutor(lifecycle.ExecutorConfig{
		BaseURL: baseURL,
		Timeout: timeout,
		Auth:    authProvider,
	})

	// Print prominent warning banner
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "\033[43m\033[30m ⚠️  LIFECYCLE TESTING MODE \033[0m")
	fmt.Fprintln(os.Stderr, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Fprintln(os.Stderr, "\033[33mWARNING: This mode creates REAL resources on the target system.\033[0m")
	fmt.Fprintln(os.Stderr, "\033[33mCleanup is attempted but may fail, leaving orphaned resources.\033[0m")
	fmt.Fprintln(os.Stderr, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Fprintln(os.Stderr, "")

	// Track summary statistics
	var totalLifecycles, passedLifecycles, failedLifecycles int
	var cleanupFailures int
	var orphanedResources []string

	for _, lc := range spec.Lifecycles {
		totalLifecycles++
		fmt.Fprintf(os.Stderr, "Starting lifecycle: %s\n", lc.Name)

		result := exec.Execute(ctx, &lc)

		// Print each step result
		for _, step := range result.Steps {
			// Determine step success based on step name
			var stepError error
			switch step.StepName {
			case "create":
				stepError = result.CreateError
			case "read":
				stepError = result.ReadError
			case "update":
				stepError = result.UpdateError
			case "cleanup":
				stepError = result.CleanupError
			}

			statusIcon := "✓"
			statusColor := "\033[32m" // green
			if step.Error != nil || stepError != nil {
				statusIcon = "✗"
				statusColor = "\033[31m" // red
			}

			// Show actual path if different from template
			displayPath := step.Path
			if step.ActualPath != "" && step.ActualPath != step.Path {
				displayPath = step.ActualPath
			}

			fmt.Fprintf(os.Stderr, "  %s%s\033[0m %s %s %s",
				statusColor,
				statusIcon,
				strings.ToUpper(step.StepName),
				step.Method,
				displayPath,
			)

			if step.StatusCode > 0 {
				fmt.Fprintf(os.Stderr, " [%d]", step.StatusCode)
			}

			if step.Duration > 0 {
				fmt.Fprintf(os.Stderr, " (%s)", step.Duration.Round(time.Millisecond))
			}

			fmt.Fprintln(os.Stderr)

			// Print step-level error if any
			if step.Error != nil {
				fmt.Fprintf(os.Stderr, "    Error: %v\n", step.Error)
			}

			// Print captured values (CREATE step only)
			if len(step.Captured) > 0 {
				fmt.Fprintln(os.Stderr, "    Captured:")
				for name, value := range step.Captured {
					displayValue := lifecycle.MaskValue(name, value)
					fmt.Fprintf(os.Stderr, "      %s = %s\n", name, displayValue)
				}
			}
		}

		// Determine lifecycle outcome and print failure details
		if result.HasError() {
			failedLifecycles++

			// Print all step errors
			if result.CreateError != nil {
				fmt.Fprintf(os.Stderr, "  \033[31m✗\033[0m CREATE FAILED: %v\n", result.CreateError)
			}
			if result.ReadError != nil {
				fmt.Fprintf(os.Stderr, "  \033[31m✗\033[0m READ FAILED: %v\n", result.ReadError)
			}
			if result.UpdateError != nil {
				fmt.Fprintf(os.Stderr, "  \033[31m✗\033[0m UPDATE FAILED: %v\n", result.UpdateError)
			}
			if result.CleanupError != nil {
				cleanupFailures++
				fmt.Fprintf(os.Stderr, "  \033[31m✗\033[0m CLEANUP FAILED: %v\n", result.CleanupError)

				// Track orphaned resources
				if len(result.CapturedVars) > 0 {
					for name, value := range result.CapturedVars {
						orphanedResources = append(orphanedResources,
							fmt.Sprintf("%s: %s=%s", lc.Name, name, lifecycle.MaskValue(name, value)))
					}
				}
			}
		} else {
			passedLifecycles++
			fmt.Fprintln(os.Stderr, "  \033[32m✓\033[0m Lifecycle PASSED")
		}

		fmt.Fprintln(os.Stderr)
	}

	// Final summary
	fmt.Fprintln(os.Stderr, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	if failedLifecycles == 0 {
		fmt.Fprintln(os.Stderr, "\033[32mPASSED\033[0m")
	} else {
		fmt.Fprintln(os.Stderr, "\033[31mFAILED\033[0m")
	}

	fmt.Fprintf(os.Stderr, "Total: %d | Passed: %d | Failed: %d",
		totalLifecycles, passedLifecycles, failedLifecycles)

	if cleanupFailures > 0 {
		fmt.Fprintf(os.Stderr, " | \033[31mCleanup failures: %d\033[0m", cleanupFailures)
	}
	fmt.Fprintln(os.Stderr)

	// Warn about orphaned resources
	if len(orphanedResources) > 0 {
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "\033[41m\033[37m ⚠️  ORPHANED RESOURCES \033[0m")
		fmt.Fprintln(os.Stderr, "\033[33mThe following resources may not have been cleaned up:\033[0m")
		for _, res := range orphanedResources {
			fmt.Fprintf(os.Stderr, "   • %s\n", res)
		}
		fmt.Fprintln(os.Stderr, "\033[33mManual cleanup is required.\033[0m")
	}

	// Exit codes:
	// 2 = cleanup failure (orphaned resources) - highest priority
	// 1 = lifecycle failure (CREATE/READ/UPDATE failed)
	// 0 = all passed
	if cleanupFailures > 0 {
		return exitCleanupFailure
	}
	if failedLifecycles > 0 {
		return exitError
	}
	return exitOK
}
