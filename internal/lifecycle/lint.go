package lifecycle

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// LintIssue represents a single linting issue found in a lifecycle spec.
type LintIssue struct {
	Line    int    // Line number (1-indexed)
	Column  int    // Column number (1-indexed, 0 if unknown)
	Path    string // YAML path (e.g., "users-crud.create.body.name")
	Message string // Human-readable message
	Value   string // The problematic value (if applicable)
}

// LintResult holds all linting issues found in a spec.
type LintResult struct {
	FilePath string
	Issues   []LintIssue
}

// HasIssues returns true if there are any linting issues.
func (r *LintResult) HasIssues() bool {
	return len(r.Issues) > 0
}

// FormatIssues returns a formatted string of all issues.
func (r *LintResult) FormatIssues() string {
	if len(r.Issues) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d issue(s) in %s:\n\n", len(r.Issues), r.FilePath))

	for _, issue := range r.Issues {
		// Format: filename:line: message
		if issue.Line > 0 {
			b.WriteString(fmt.Sprintf("  %s:%d: %s\n", r.FilePath, issue.Line, issue.Message))
		} else {
			b.WriteString(fmt.Sprintf("  %s: %s\n", r.FilePath, issue.Message))
		}

		// Show the path context
		if issue.Path != "" {
			b.WriteString(fmt.Sprintf("    Path: %s\n", issue.Path))
		}

		// Show the value if relevant
		if issue.Value != "" {
			b.WriteString(fmt.Sprintf("    Value: %q\n", issue.Value))
		}
		b.WriteString("\n")
	}

	return b.String()
}

// LintFile lints a lifecycle spec file and returns detailed issues.
func LintFile(path string) (*LintResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	result := &LintResult{
		FilePath: path,
	}

	// Find TODO values with line numbers
	todoIssues := findTODOsWithLines(data)
	result.Issues = append(result.Issues, todoIssues...)

	// Sort issues by line number
	sort.Slice(result.Issues, func(i, j int) bool {
		return result.Issues[i].Line < result.Issues[j].Line
	})

	return result, nil
}

// findTODOsWithLines scans the file content for TODO values and returns issues with line numbers.
func findTODOsWithLines(data []byte) []LintIssue {
	var issues []LintIssue
	seenLines := make(map[int]bool) // Avoid duplicate issues on same line

	// Pattern to match TODO values in YAML
	// Matches: key: "TODO" or key: 'TODO' or key: TODO
	todoValuePattern := regexp.MustCompile(`^\s*(\w+):\s*["']?TODO["']?\s*$`)

	// Pattern to match TODO in quoted strings (like description)
	todoInStringPattern := regexp.MustCompile(`:\s*["'].*TODO.*["']`)

	// Track current YAML path context
	var currentLifecycle string
	var currentStep string
	indentStack := []int{0}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Skip empty lines and pure comments
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Calculate indent level
		indent := len(line) - len(strings.TrimLeft(line, " "))

		// Track lifecycle context
		if strings.HasPrefix(trimmed, "- name:") {
			currentLifecycle = strings.TrimSpace(strings.TrimPrefix(trimmed, "- name:"))
			currentLifecycle = strings.Trim(currentLifecycle, `"'`)
			currentStep = ""
		}

		// Track step context
		for _, step := range []string{"create:", "read:", "update:", "cleanup:"} {
			if strings.HasPrefix(trimmed, step) {
				currentStep = strings.TrimSuffix(step, ":")
				break
			}
		}

		// Skip if we already reported an issue on this line
		if seenLines[lineNum] {
			continue
		}

		// Check for exact TODO value assignment (key: "TODO" or key: TODO)
		if matches := todoValuePattern.FindStringSubmatch(line); matches != nil {
			key := matches[1]
			path := buildPath(currentLifecycle, currentStep, key, indent, indentStack)

			issues = append(issues, LintIssue{
				Line:    lineNum,
				Path:    path,
				Message: "TODO placeholder must be replaced with actual value",
				Value:   "TODO",
			})
			seenLines[lineNum] = true
			continue
		}

		// Check for TODO in quoted strings (like description: "TODO: something")
		if todoInStringPattern.MatchString(line) {
			// Extract the key
			if colonIdx := strings.Index(trimmed, ":"); colonIdx > 0 {
				key := strings.TrimSpace(trimmed[:colonIdx])
				key = strings.TrimPrefix(key, "- ")
				path := buildPath(currentLifecycle, currentStep, key, indent, indentStack)

				issues = append(issues, LintIssue{
					Line:    lineNum,
					Path:    path,
					Message: "TODO placeholder found in value",
					Value:   extractTODOContext(trimmed),
				})
				seenLines[lineNum] = true
			}
		}

		// Update indent stack for path tracking
		if indent > indentStack[len(indentStack)-1] {
			indentStack = append(indentStack, indent)
		} else {
			for len(indentStack) > 1 && indent <= indentStack[len(indentStack)-1] {
				indentStack = indentStack[:len(indentStack)-1]
			}
		}
	}

	return issues
}

// buildPath builds a YAML-like path for the current location.
func buildPath(lifecycle, step, key string, indent int, indentStack []int) string {
	var parts []string

	if lifecycle != "" {
		parts = append(parts, lifecycle)
	}
	if step != "" {
		parts = append(parts, step)
	}
	if key != "" && key != "name" && key != "description" {
		// Check if we're in a body or capture section
		if indent > 6 { // Nested under body or capture
			parts = append(parts, "body", key)
		} else {
			parts = append(parts, key)
		}
	} else if key == "description" {
		parts = append(parts, key)
	}

	return strings.Join(parts, ".")
}

// extractTODOContext extracts context around TODO in a line.
func extractTODOContext(line string) string {
	if idx := strings.Index(line, ":"); idx > 0 {
		value := strings.TrimSpace(line[idx+1:])
		value = strings.Trim(value, `"'`)
		if len(value) > 50 {
			return value[:50] + "..."
		}
		return value
	}
	return ""
}

// ValidateWithLint performs validation and returns detailed lint results.
// This is used for better error messages.
func ValidateWithLint(path string) (*Spec, *LintResult, error) {
	// First lint the file
	lintResult, err := LintFile(path)
	if err != nil {
		return nil, nil, err
	}

	// If there are lint issues, return them without loading
	if lintResult.HasIssues() {
		return nil, lintResult, nil
	}

	// Otherwise, load normally
	spec, err := LoadSpec(path)
	if err != nil {
		return nil, nil, err
	}

	return spec, nil, nil
}
