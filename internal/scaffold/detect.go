package scaffold

import (
	"regexp"
	"sort"
	"strings"
)

// idParamPattern matches {id}, {userId}, {user_id}, etc.
var idParamPattern = regexp.MustCompile(`\{([^}]*[iI][dD][^}]*|[iI][dD])\}`)

// DetectResources detects lifecycle candidate resource groups from OpenAPI spec.
func DetectResources(spec *OpenAPISpec) ([]ResourceGroup, error) {
	endpoints := spec.ExtractEndpoints()

	// Group endpoints by resource
	resourceMap := make(map[string]*ResourceGroup)

	for _, ep := range endpoints {
		resourceName := extractResourceName(ep.Path)
		if resourceName == "" {
			continue
		}

		group, exists := resourceMap[resourceName]
		if !exists {
			group = &ResourceGroup{
				Name:     resourceName,
				BasePath: extractBasePath(ep.Path),
			}
			resourceMap[resourceName] = group
		}

		// Categorize endpoint
		categorizeEndpoint(group, ep)
	}

	// Convert map to slice and filter valid groups
	var groups []ResourceGroup
	for _, group := range resourceMap {
		// Must have at least a CREATE candidate to be a valid lifecycle
		if group.CreateEndpoint != nil {
			// Add warnings for missing endpoints
			if group.DeleteEndpoint == nil {
				group.Warnings = append(group.Warnings,
					"No DELETE endpoint found. Manual cleanup will be required.")
			}
			groups = append(groups, *group)
		}
	}

	// Sort by name for consistent output
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Name < groups[j].Name
	})

	return groups, nil
}

// extractResourceName extracts the resource name from a path.
// Examples:
//   /api/v1/users         → "users"
//   /api/v1/users/{id}    → "users"
//   /users/{userId}/posts → "posts" (innermost resource)
func extractResourceName(path string) string {
	// Remove leading/trailing slashes and split
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")

	// Work backwards to find the resource name
	// Skip version prefixes, parameter placeholders
	for i := len(parts) - 1; i >= 0; i-- {
		part := parts[i]

		// Skip path parameters
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			continue
		}

		// Skip common prefixes
		lower := strings.ToLower(part)
		if lower == "api" || lower == "v1" || lower == "v2" || lower == "v3" {
			continue
		}

		// Skip single-letter segments
		if len(part) <= 1 {
			continue
		}

		return part
	}

	return ""
}

// extractBasePath extracts the base path for a resource.
// Examples:
//   /api/v1/users/{id} → /api/v1/users
//   /users/{userId}    → /users
func extractBasePath(path string) string {
	// Remove ID parameter suffix
	if idx := strings.LastIndex(path, "/{"); idx != -1 {
		return path[:idx]
	}
	return path
}

// categorizeEndpoint categorizes an endpoint into CREATE, READ, or DELETE.
func categorizeEndpoint(group *ResourceGroup, ep Endpoint) {
	hasIDParam := hasIDParameter(ep.Path)

	switch ep.Method {
	case "POST":
		// CREATE: POST without ID parameter
		if !hasIDParam {
			if group.CreateEndpoint == nil || isBetterCreate(ep, *group.CreateEndpoint) {
				epCopy := ep
				group.CreateEndpoint = &epCopy
			}
		}

	case "GET":
		// READ: GET with ID parameter
		if hasIDParam && ep.IDParam != "" {
			if group.ReadEndpoint == nil || isBetterRead(ep, *group.ReadEndpoint) {
				epCopy := ep
				group.ReadEndpoint = &epCopy
			}
		}

	case "DELETE":
		// DELETE: DELETE with ID parameter
		if hasIDParam && ep.IDParam != "" {
			if group.DeleteEndpoint == nil || isBetterDelete(ep, *group.DeleteEndpoint) {
				epCopy := ep
				group.DeleteEndpoint = &epCopy
			}
		}
	}
}

// hasIDParameter checks if a path contains an ID parameter.
func hasIDParameter(path string) bool {
	return idParamPattern.MatchString(path)
}

// isBetterCreate returns true if ep is a better CREATE candidate than current.
// Prefers: shorter paths, paths ending with resource name, 201 responses.
func isBetterCreate(ep, current Endpoint) bool {
	// Prefer endpoints with 201 response
	epHas201 := hasStatusCode(ep.StatusCodes, 201)
	currentHas201 := hasStatusCode(current.StatusCodes, 201)
	if epHas201 && !currentHas201 {
		return true
	}
	if !epHas201 && currentHas201 {
		return false
	}

	// Prefer shorter paths
	return len(ep.Path) < len(current.Path)
}

// isBetterRead returns true if ep is a better READ candidate than current.
// Prefers: paths with single ID param, shorter paths.
func isBetterRead(ep, current Endpoint) bool {
	// Count ID parameters
	epCount := countIDParams(ep.Path)
	currentCount := countIDParams(current.Path)

	// Prefer single ID param
	if epCount == 1 && currentCount > 1 {
		return true
	}
	if epCount > 1 && currentCount == 1 {
		return false
	}

	// Prefer shorter paths
	return len(ep.Path) < len(current.Path)
}

// isBetterDelete returns true if ep is a better DELETE candidate than current.
func isBetterDelete(ep, current Endpoint) bool {
	// Same logic as READ
	return isBetterRead(ep, current)
}

// hasStatusCode checks if a status code is in the list.
func hasStatusCode(codes []int, target int) bool {
	for _, c := range codes {
		if c == target {
			return true
		}
	}
	return false
}

// countIDParams counts ID-like parameters in a path.
func countIDParams(path string) int {
	matches := idParamPattern.FindAllString(path, -1)
	return len(matches)
}
