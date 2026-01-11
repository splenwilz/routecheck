package lifecycle

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// captureValues extracts values from a JSON response body using JSONPath-like expressions.
// Supports simple path expressions like:
//   - $.id
//   - $.data.user.id
//   - $.items[0].id
func captureValues(body string, captures map[string]string) (map[string]string, error) {
	if len(captures) == 0 {
		return nil, fmt.Errorf("no capture expressions defined")
	}

	// Parse JSON body
	var data any
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		return nil, fmt.Errorf("failed to parse response body as JSON: %w", err)
	}

	result := make(map[string]string)

	for varName, expr := range captures {
		value, err := extractValue(data, expr)
		if err != nil {
			return nil, fmt.Errorf("capture '%s' with expression '%s': %w", varName, expr, err)
		}
		result[varName] = value
	}

	return result, nil
}

// extractValue extracts a value from parsed JSON using a JSONPath-like expression.
func extractValue(data any, expr string) (string, error) {
	// Must start with $
	if !strings.HasPrefix(expr, "$") {
		return "", fmt.Errorf("expression must start with '$'")
	}

	// Remove leading $
	path := strings.TrimPrefix(expr, "$")
	if path == "" {
		return valueToString(data)
	}

	// Remove leading dot if present
	path = strings.TrimPrefix(path, ".")

	// Navigate the path
	current := data
	parts := splitPath(path)

	for _, part := range parts {
		if part == "" {
			continue
		}

		// Check for array index: field[0]
		if idx := strings.Index(part, "["); idx != -1 {
			// Get field name before bracket
			fieldName := part[:idx]

			// Navigate to field first if there's a field name
			if fieldName != "" {
				obj, ok := current.(map[string]any)
				if !ok {
					return "", fmt.Errorf("expected object at '%s', got %T", fieldName, current)
				}
				val, exists := obj[fieldName]
				if !exists {
					return "", fmt.Errorf("field '%s' not found", fieldName)
				}
				current = val
			}

			// Extract array index
			closeIdx := strings.Index(part, "]")
			if closeIdx == -1 {
				return "", fmt.Errorf("unclosed bracket in '%s'", part)
			}
			indexStr := part[idx+1 : closeIdx]
			index, err := strconv.Atoi(indexStr)
			if err != nil {
				return "", fmt.Errorf("invalid array index '%s'", indexStr)
			}

			// Navigate array
			arr, ok := current.([]any)
			if !ok {
				return "", fmt.Errorf("expected array, got %T", current)
			}
			if index < 0 || index >= len(arr) {
				return "", fmt.Errorf("array index %d out of bounds (length %d)", index, len(arr))
			}
			current = arr[index]
		} else {
			// Simple field access
			obj, ok := current.(map[string]any)
			if !ok {
				return "", fmt.Errorf("expected object at '%s', got %T", part, current)
			}
			val, exists := obj[part]
			if !exists {
				return "", fmt.Errorf("field '%s' not found", part)
			}
			current = val
		}
	}

	return valueToString(current)
}

// splitPath splits a JSONPath into parts, handling dots correctly.
func splitPath(path string) []string {
	var parts []string
	var current strings.Builder

	for i := 0; i < len(path); i++ {
		c := path[i]
		if c == '.' {
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
		} else {
			current.WriteByte(c)
		}
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

// valueToString converts a JSON value to its string representation.
func valueToString(v any) (string, error) {
	switch val := v.(type) {
	case string:
		return val, nil
	case float64:
		// Check if it's an integer
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10), nil
		}
		return strconv.FormatFloat(val, 'f', -1, 64), nil
	case int:
		return strconv.Itoa(val), nil
	case int64:
		return strconv.FormatInt(val, 10), nil
	case bool:
		return strconv.FormatBool(val), nil
	case nil:
		return "", fmt.Errorf("value is null")
	default:
		// For complex types, return JSON representation
		bytes, err := json.Marshal(val)
		if err != nil {
			return "", fmt.Errorf("failed to convert value to string: %w", err)
		}
		return string(bytes), nil
	}
}

// MaskValue masks a captured value if it appears to be sensitive.
func MaskValue(name, value string) string {
	// List of sensitive field names (case-insensitive check)
	sensitiveNames := []string{
		"token", "secret", "password", "key", "auth",
		"credential", "api_key", "apikey", "access_token",
	}

	nameLower := strings.ToLower(name)
	for _, sensitive := range sensitiveNames {
		if strings.Contains(nameLower, sensitive) {
			if len(value) <= 8 {
				return "***"
			}
			return value[:4] + "***" + value[len(value)-4:]
		}
	}

	return value
}

// InterpolatePath replaces {{variable}} placeholders in a path with captured values.
// Returns the interpolated path and any error if a variable is not found.
func InterpolatePath(path string, captured map[string]string) (string, error) {
	// Match {{variable_name}} pattern
	pattern := regexp.MustCompile(`\{\{([^}]+)\}\}`)

	var missingVars []string
	result := pattern.ReplaceAllStringFunc(path, func(match string) string {
		// Extract variable name (remove {{ and }})
		varName := strings.TrimPrefix(match, "{{")
		varName = strings.TrimSuffix(varName, "}}")
		varName = strings.TrimSpace(varName)

		if value, ok := captured[varName]; ok {
			return value
		}
		missingVars = append(missingVars, varName)
		return match // Keep original if not found
	})

	if len(missingVars) > 0 {
		return "", fmt.Errorf("missing captured variables: %s", strings.Join(missingVars, ", "))
	}

	return result, nil
}
