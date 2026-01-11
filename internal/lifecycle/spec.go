// Package lifecycle handles lifecycle testing specification parsing and validation.
// Phase 0: Only structure validation, no execution.
package lifecycle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Spec represents a lifecycle testing specification file.
type Spec struct {
	Lifecycles []Lifecycle `json:"lifecycles" yaml:"lifecycles"`
}

// Lifecycle represents a single CRUD lifecycle test.
type Lifecycle struct {
	Name        string  `json:"name" yaml:"name"`
	Description string  `json:"description,omitempty" yaml:"description,omitempty"`
	Create      *Step   `json:"create" yaml:"create"`
	Read        *Step   `json:"read,omitempty" yaml:"read,omitempty"`
	Update      *Step   `json:"update,omitempty" yaml:"update,omitempty"`
	Cleanup     *Step   `json:"cleanup" yaml:"cleanup"`
}

// Step represents a single step in a lifecycle.
type Step struct {
	Method         string            `json:"method" yaml:"method"`
	Path           string            `json:"path" yaml:"path"`
	Body           map[string]any    `json:"body,omitempty" yaml:"body,omitempty"`
	Headers        map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	ExpectedStatus []int             `json:"expected_status,omitempty" yaml:"expected_status,omitempty"`
	Capture        map[string]string `json:"capture,omitempty" yaml:"capture,omitempty"`
}

// LoadSpec loads and validates a lifecycle spec from a file.
// Returns an error if the file cannot be read or parsed.
func LoadSpec(path string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("lifecycle file not found: %s", path)
		}
		return nil, fmt.Errorf("failed to read lifecycle file: %w", err)
	}

	spec, err := parseSpec(path, data)
	if err != nil {
		return nil, err
	}

	if err := validateSpec(spec); err != nil {
		return nil, err
	}

	return spec, nil
}

// parseSpec parses YAML or JSON based on file extension.
func parseSpec(path string, data []byte) (*Spec, error) {
	var spec Spec
	ext := strings.ToLower(filepath.Ext(path))

	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &spec); err != nil {
			return nil, fmt.Errorf("failed to parse lifecycle YAML: %w", err)
		}
	case ".json":
		if err := json.Unmarshal(data, &spec); err != nil {
			return nil, fmt.Errorf("failed to parse lifecycle JSON: %w", err)
		}
	default:
		// Try YAML first (superset of JSON)
		if err := yaml.Unmarshal(data, &spec); err != nil {
			if jsonErr := json.Unmarshal(data, &spec); jsonErr != nil {
				return nil, fmt.Errorf("failed to parse lifecycle file (tried YAML and JSON): invalid format")
			}
		}
	}

	return &spec, nil
}

// validateSpec validates the lifecycle spec structure.
// This is Phase 0 validation - structure only, no execution logic.
func validateSpec(spec *Spec) error {
	if len(spec.Lifecycles) == 0 {
		return fmt.Errorf("lifecycle spec has no lifecycles defined")
	}

	for i, lc := range spec.Lifecycles {
		if err := validateLifecycle(i, &lc); err != nil {
			return err
		}
	}

	// Check for unresolved TODO placeholders
	if todos := findTODOs(spec); len(todos) > 0 {
		return fmt.Errorf("lifecycle spec contains unresolved TODOs:\n%s\n\nPlease fill in all TODO fields before running lifecycle tests", strings.Join(todos, "\n"))
	}

	return nil
}

// findTODOs finds all TODO placeholders in the spec.
// Returns a list of locations where TODOs were found.
func findTODOs(spec *Spec) []string {
	var todos []string

	for _, lc := range spec.Lifecycles {
		// Check description
		if strings.Contains(lc.Description, "TODO") {
			todos = append(todos, fmt.Sprintf("  %s.description: contains TODO", lc.Name))
		}

		// Check steps
		if lc.Create != nil {
			todos = append(todos, findStepTODOs(lc.Name, "create", lc.Create)...)
		}
		if lc.Read != nil {
			todos = append(todos, findStepTODOs(lc.Name, "read", lc.Read)...)
		}
		if lc.Update != nil {
			todos = append(todos, findStepTODOs(lc.Name, "update", lc.Update)...)
		}
		if lc.Cleanup != nil {
			todos = append(todos, findStepTODOs(lc.Name, "cleanup", lc.Cleanup)...)
		}
	}

	return todos
}

// findStepTODOs finds TODO placeholders in a step.
func findStepTODOs(lifecycleName, stepName string, step *Step) []string {
	var todos []string
	prefix := fmt.Sprintf("  %s.%s", lifecycleName, stepName)

	// Check body fields for TODO values
	for key, value := range step.Body {
		if str, ok := value.(string); ok && str == "TODO" {
			todos = append(todos, fmt.Sprintf("%s.body.%s: \"TODO\"", prefix, key))
		}
	}

	// Check capture paths for TODO
	for key, path := range step.Capture {
		if strings.Contains(path, "TODO") {
			todos = append(todos, fmt.Sprintf("%s.capture.%s: contains TODO", prefix, key))
		}
	}

	return todos
}

// validateLifecycle validates a single lifecycle definition.
func validateLifecycle(index int, lc *Lifecycle) error {
	identifier := lc.Name
	if identifier == "" {
		identifier = fmt.Sprintf("lifecycle %d", index+1)
	}

	// Name is required
	if lc.Name == "" {
		return fmt.Errorf("%s: 'name' is required", identifier)
	}

	// Create step is required
	if lc.Create == nil {
		return fmt.Errorf("lifecycle '%s': 'create' step is required", lc.Name)
	}

	// Cleanup step is required
	if lc.Cleanup == nil {
		return fmt.Errorf("lifecycle '%s': 'cleanup' step is required", lc.Name)
	}

	// Validate create step
	if err := validateStep(lc.Name, "create", lc.Create, true); err != nil {
		return err
	}

	// Create must have capture
	if len(lc.Create.Capture) == 0 {
		return fmt.Errorf("lifecycle '%s': 'create' step must have 'capture' to extract resource ID for cleanup", lc.Name)
	}

	// Validate cleanup step
	if err := validateStep(lc.Name, "cleanup", lc.Cleanup, false); err != nil {
		return err
	}

	// Validate optional steps
	if lc.Read != nil {
		if err := validateStep(lc.Name, "read", lc.Read, false); err != nil {
			return err
		}
	}

	if lc.Update != nil {
		if err := validateStep(lc.Name, "update", lc.Update, false); err != nil {
			return err
		}
	}

	return nil
}

// validateStep validates a single step definition.
func validateStep(lifecycleName, stepName string, step *Step, requireCapture bool) error {
	prefix := fmt.Sprintf("lifecycle '%s' %s step", lifecycleName, stepName)

	if step.Method == "" {
		return fmt.Errorf("%s: 'method' is required", prefix)
	}

	validMethods := map[string]bool{
		"GET": true, "POST": true, "PUT": true, "PATCH": true,
		"DELETE": true, "HEAD": true, "OPTIONS": true,
	}
	method := strings.ToUpper(step.Method)
	if !validMethods[method] {
		return fmt.Errorf("%s: invalid method '%s'", prefix, step.Method)
	}

	if step.Path == "" {
		return fmt.Errorf("%s: 'path' is required", prefix)
	}

	if requireCapture && len(step.Capture) == 0 {
		return fmt.Errorf("%s: 'capture' is required", prefix)
	}

	return nil
}
