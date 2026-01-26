package auth

import (
	"strings"

	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
)

type SecurityRequirements struct {
	kind string
	name string
	reqs []kdexv1alpha1.SecurityRequirement
}

func NewSecurityRequirements(kind string, name string) *SecurityRequirements {
	return &SecurityRequirements{
		kind: kind,
		name: name,
	}
}

func (sr *SecurityRequirements) With(req ...[]kdexv1alpha1.SecurityRequirement) *SecurityRequirements {
	for _, r := range req {
		sr.reqs = append(sr.reqs, r...)
	}
	return sr
}

func (sr *SecurityRequirements) IsEmpty() bool {
	if sr == nil || len(sr.reqs) == 0 {
		return true
	}
	return false
}

// Validate checks if the user's scopes satisfy the security requirements.
func (sr *SecurityRequirements) Validate(
	userScopes []string,
) bool {
	// If no requirements, access is granted
	if sr.IsEmpty() {
		return true
	}

	// Requirements are AND'ed - user needs to satisfy all requirements
	for _, requirement := range sr.reqs {
		if !sr.satisfiesRequirement(requirement, userScopes) {
			return false
		}
	}

	return true
}

// satisfiesRequirement checks if user scopes satisfy a single security requirement.
// Within a requirement, all scopes must be present (AND logic).
func (sr *SecurityRequirements) satisfiesRequirement(
	requirement kdexv1alpha1.SecurityRequirement,
	userScopes []string,
) bool {
	// Flatten all required scopes from the requirement
	var requiredScopes []string
	for _, scopes := range requirement {
		requiredScopes = append(requiredScopes, scopes...)
	}

	// Check if user has all required scopes
	for _, requiredScope := range requiredScopes {
		if !sr.hasScope(userScopes, requiredScope) {
			return false
		}
	}

	return true
}

// hasScope checks if the user has a specific scope.
// Supports exact match and wildcard patterns.
// Format: resource:resourceName:verb
// Examples:
//   - "pages::read" - read access to all pages
//   - "pages:home:read" - read access to specific page "home"
//   - "pages:*:read" - read access to all pages (explicit wildcard)
func (sr *SecurityRequirements) hasScope(userScopes []string, requiredScope string) bool {
	for _, userScope := range userScopes {
		if sr.scopeMatches(userScope, requiredScope) {
			return true
		}
	}
	return false
}

// scopeMatches checks if a user scope matches a required scope.
// Supports wildcards in resource name position.
func (sr *SecurityRequirements) scopeMatches(userScope, requiredScope string) bool {
	// Exact match
	if userScope == requiredScope {
		return true
	}

	// Parse scopes
	userParts := strings.Split(userScope, ":")
	requiredParts := strings.Split(requiredScope, ":")

	// Must have same structure (resource:resourceName:verb)
	if len(userParts) != 3 || len(requiredParts) != 3 {
		return false
	}

	// Resource type must match
	if userParts[0] != requiredParts[0] {
		return false
	}

	// Verb must match
	if userParts[2] != requiredParts[2] {
		return false
	}

	// Check resource name with wildcard support
	// Empty string or "*" in user scope means all resources
	if userParts[1] == "" || userParts[1] == "*" {
		return true
	}

	// Specific resource name must match
	return userParts[1] == requiredParts[1]
}
