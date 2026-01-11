package lifecycle

import (
	"fmt"
	"strings"
)

// ExplainResult holds the explanation of a lifecycle spec.
type ExplainResult struct {
	FilePath   string
	Lifecycles []LifecycleExplanation
	Summary    ExplainSummary
}

// LifecycleExplanation explains a single lifecycle.
type LifecycleExplanation struct {
	Name        string
	Description string
	Steps       []StepExplanation
	Warnings    []string
}

// StepExplanation explains a single step.
type StepExplanation struct {
	Name           string
	Method         string
	Path           string
	HasBody        bool
	BodyFields     []string
	CaptureVars    []string
	ExpectedStatus []int
	UsesVariables  []string // Variables used from previous steps
}

// ExplainSummary provides a high-level summary.
type ExplainSummary struct {
	TotalLifecycles   int
	TotalSteps        int
	MutationCount     int // POST, PUT, PATCH, DELETE
	ReadOnlyCount     int // GET, HEAD, OPTIONS
	CapturedVariables []string
	Warnings          []string
}

// Explain analyzes a lifecycle spec and returns a detailed explanation.
func Explain(spec *Spec, filePath string) *ExplainResult {
	result := &ExplainResult{
		FilePath: filePath,
	}

	allCaptured := make(map[string]bool)

	for _, lc := range spec.Lifecycles {
		lcExplain := LifecycleExplanation{
			Name:        lc.Name,
			Description: lc.Description,
		}

		// Track captured variables for this lifecycle
		lifecycleCaptured := make(map[string]bool)

		// Explain each step
		if lc.Create != nil {
			step := explainStep("CREATE", lc.Create, lifecycleCaptured)
			lcExplain.Steps = append(lcExplain.Steps, step)
			result.Summary.TotalSteps++
			result.Summary.MutationCount++

			// Track captured variables
			for _, v := range step.CaptureVars {
				lifecycleCaptured[v] = true
				allCaptured[v] = true
			}
		}

		if lc.Read != nil {
			step := explainStep("READ", lc.Read, lifecycleCaptured)
			lcExplain.Steps = append(lcExplain.Steps, step)
			result.Summary.TotalSteps++
			result.Summary.ReadOnlyCount++
		}

		if lc.Update != nil {
			step := explainStep("UPDATE", lc.Update, lifecycleCaptured)
			lcExplain.Steps = append(lcExplain.Steps, step)
			result.Summary.TotalSteps++
			result.Summary.MutationCount++
		}

		if lc.Cleanup != nil {
			step := explainStep("CLEANUP", lc.Cleanup, lifecycleCaptured)
			lcExplain.Steps = append(lcExplain.Steps, step)
			result.Summary.TotalSteps++
			result.Summary.MutationCount++
		} else {
			lcExplain.Warnings = append(lcExplain.Warnings, "No CLEANUP step defined")
		}

		// Check for potential issues
		if lc.Create != nil && len(lc.Create.Capture) == 0 {
			lcExplain.Warnings = append(lcExplain.Warnings,
				"CREATE step has no capture - subsequent steps may fail")
		}

		result.Lifecycles = append(result.Lifecycles, lcExplain)
	}

	result.Summary.TotalLifecycles = len(spec.Lifecycles)

	// Collect all captured variables
	for v := range allCaptured {
		result.Summary.CapturedVariables = append(result.Summary.CapturedVariables, v)
	}

	return result
}

// explainStep creates an explanation for a single step.
func explainStep(name string, step *Step, captured map[string]bool) StepExplanation {
	explanation := StepExplanation{
		Name:           name,
		Method:         strings.ToUpper(step.Method),
		Path:           step.Path,
		HasBody:        len(step.Body) > 0,
		ExpectedStatus: step.ExpectedStatus,
	}

	// List body fields
	for key := range step.Body {
		explanation.BodyFields = append(explanation.BodyFields, key)
	}

	// List capture variables
	for key := range step.Capture {
		explanation.CaptureVars = append(explanation.CaptureVars, key)
	}

	// Find used variables ({{var}} syntax)
	usedVars := findUsedVariables(step.Path)
	for _, v := range usedVars {
		if captured[v] {
			explanation.UsesVariables = append(explanation.UsesVariables, v)
		}
	}

	return explanation
}

// findUsedVariables extracts variable names from {{var}} syntax.
func findUsedVariables(s string) []string {
	var vars []string
	for {
		start := strings.Index(s, "{{")
		if start == -1 {
			break
		}
		end := strings.Index(s[start:], "}}")
		if end == -1 {
			break
		}
		varName := s[start+2 : start+end]
		vars = append(vars, varName)
		s = s[start+end+2:]
	}
	return vars
}

// FormatExplanation formats the explanation for CLI output.
func (r *ExplainResult) FormatExplanation() string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("Lifecycle Spec: %s\n", r.FilePath))
	b.WriteString(strings.Repeat("=", 60) + "\n\n")

	for _, lc := range r.Lifecycles {
		b.WriteString(fmt.Sprintf("Lifecycle: %s\n", lc.Name))
		if lc.Description != "" && !strings.Contains(lc.Description, "TODO") {
			b.WriteString(fmt.Sprintf("  %s\n", lc.Description))
		}
		b.WriteString(strings.Repeat("-", 40) + "\n")

		// Show execution flow
		b.WriteString("\n  Execution Flow:\n")
		for i, step := range lc.Steps {
			arrow := "  "
			if i > 0 {
				arrow = "→ "
			}
			b.WriteString(fmt.Sprintf("    %s%s %s %s\n", arrow, step.Name, step.Method, step.Path))

			// Show what this step does
			if len(step.CaptureVars) > 0 {
				b.WriteString(fmt.Sprintf("      Captures: %s\n", strings.Join(step.CaptureVars, ", ")))
			}
			if len(step.UsesVariables) > 0 {
				b.WriteString(fmt.Sprintf("      Uses: %s\n", strings.Join(step.UsesVariables, ", ")))
			}
			if len(step.ExpectedStatus) > 0 {
				b.WriteString(fmt.Sprintf("      Expects: %v\n", step.ExpectedStatus))
			}
		}

		// Show warnings
		if len(lc.Warnings) > 0 {
			b.WriteString("\n  Warnings:\n")
			for _, w := range lc.Warnings {
				b.WriteString(fmt.Sprintf("    ⚠ %s\n", w))
			}
		}

		b.WriteString("\n")
	}

	// Summary
	b.WriteString(strings.Repeat("=", 60) + "\n")
	b.WriteString("Summary\n")
	b.WriteString(strings.Repeat("-", 40) + "\n")
	b.WriteString(fmt.Sprintf("  Lifecycles:  %d\n", r.Summary.TotalLifecycles))
	b.WriteString(fmt.Sprintf("  Total Steps: %d\n", r.Summary.TotalSteps))
	b.WriteString(fmt.Sprintf("  Mutations:   %d (POST, PUT, PATCH, DELETE)\n", r.Summary.MutationCount))
	b.WriteString(fmt.Sprintf("  Read-only:   %d (GET)\n", r.Summary.ReadOnlyCount))

	if len(r.Summary.CapturedVariables) > 0 {
		b.WriteString(fmt.Sprintf("  Variables:   %s\n", strings.Join(r.Summary.CapturedVariables, ", ")))
	}

	return b.String()
}

// FormatPreExecutionSummary formats a brief summary shown before execution.
func (r *ExplainResult) FormatPreExecutionSummary() string {
	var b strings.Builder

	b.WriteString("┌─────────────────────────────────────────────────────────────┐\n")
	b.WriteString("│                    LIFECYCLE EXECUTION PLAN                 │\n")
	b.WriteString("├─────────────────────────────────────────────────────────────┤\n")

	for _, lc := range r.Lifecycles {
		b.WriteString(fmt.Sprintf("│ %s\n", lc.Name))

		for i, step := range lc.Steps {
			prefix := "│   "
			if i == len(lc.Steps)-1 {
				prefix = "│   "
			}
			b.WriteString(fmt.Sprintf("%s%d. %s %s %s\n", prefix, i+1, step.Name, step.Method, truncatePath(step.Path, 35)))
		}
		b.WriteString("│\n")
	}

	b.WriteString("├─────────────────────────────────────────────────────────────┤\n")
	b.WriteString(fmt.Sprintf("│ Total: %d lifecycle(s), %d mutation(s), %d read(s)%s│\n",
		r.Summary.TotalLifecycles,
		r.Summary.MutationCount,
		r.Summary.ReadOnlyCount,
		strings.Repeat(" ", 14)))
	b.WriteString("└─────────────────────────────────────────────────────────────┘\n")

	return b.String()
}

// truncatePath truncates a path to fit in a column.
func truncatePath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}
	return path[:maxLen-3] + "..."
}
