package auth

import (
	"context"
	"strings"

	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
)

// AuthorizationChecker validates whether a user has the required permissions.
type AuthorizationChecker struct{}

// NewAuthorizationChecker creates a new authorization checker.
func NewAuthorizationChecker() *AuthorizationChecker {
	return &AuthorizationChecker{}
}

// CheckPageAccess validates whether the user has access to a page based on security requirements.
// It checks page-level security first, falling back to host-level security if page security is not defined.
// Returns true if access is granted, false otherwise.
func (ac *AuthorizationChecker) CheckPageAccess(
	ctx context.Context,
	pageSecurity *[]kdexv1alpha1.SecurityRequirement,
	hostSecurity *[]kdexv1alpha1.SecurityRequirement,
) (bool, error) {
	// Determine which security requirements to use
	var securityReqs *[]kdexv1alpha1.SecurityRequirement
	if pageSecurity != nil && len(*pageSecurity) > 0 {
		securityReqs = pageSecurity
	} else if hostSecurity != nil && len(*hostSecurity) > 0 {
		securityReqs = hostSecurity
	} else {
		// No security requirements = public access
		return true, nil
	}

	// Get claims from context
	claims, hasClaims := GetClaims(ctx)
	if !hasClaims {
		// Security requirements exist but no auth context = unauthorized
		return false, nil
	}

	// Check if user has required scopes
	return ac.validateSecurityRequirements(*securityReqs, claims.Scopes), nil
}

// validateSecurityRequirements checks if the user's scopes satisfy the security requirements.
// SecurityRequirement is a map where keys are security scheme names and values are required scopes.
// For our implementation, we expect a key like "oauth2" or "jwt" with scope values.
func (ac *AuthorizationChecker) validateSecurityRequirements(
	requirements []kdexv1alpha1.SecurityRequirement,
	userScopes []string,
) bool {
	// If no requirements, access is granted
	if len(requirements) == 0 {
		// This is a safety net incase the method is called standalone, which should bever happpen
		return true
	}

	// Requirements are OR'd - user needs to satisfy at least one
	for _, requirement := range requirements {
		if ac.satisfiesRequirement(requirement, userScopes) {
			return true
		}
	}

	return false
}

// satisfiesRequirement checks if user scopes satisfy a single security requirement.
// Within a requirement, all scopes must be present (AND logic).
func (ac *AuthorizationChecker) satisfiesRequirement(
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
		if !ac.hasScope(userScopes, requiredScope) {
			return false
		}
	}

	return true
}

// hasScope checks if the user has a specific scope.
// Supports exact match and wildcard patterns.
// Format:     resource:resourceName:verb
// Short Form: resource:verb (means wildcard resourceName)
// Examples:
//   - "pages::read" - read access to all pages
//   - "pages:home:read" - read access to specific page "home"
//   - "pages:*:read" - read access to all pages (explicit wildcard)
//   - "pages:home:all" - all access to page "home" (wildcard verb)
//   - "pages:all" - (short form) all access to all pages (wildcard resource name and verb)
//   - "pages::all" - same as above
//   - "pages:*:all" - same as above
func (ac *AuthorizationChecker) hasScope(userScopes []string, requiredScope string) bool {
	for _, userScope := range userScopes {
		if ac.scopeMatches(userScope, requiredScope) {
			return true
		}
	}
	return false
}

// scopeMatches checks if a user scope matches a required scope.
// Supports wildcards in resource name position.
func (ac *AuthorizationChecker) scopeMatches(userScope, requiredScope string) bool {
	// Exact match
	if userScope == requiredScope {
		return true
	}

	// Parse scopes
	userParts := strings.Split(userScope, ":")

	if len(userParts) == 2 {
		// short syntax was used <resource>:<verb> which is equal to <resource>::<verb>, or <resource>:*:<verb>
		userParts = []string{userParts[0], "", userParts[1]}
	}

	requiredParts := strings.Split(requiredScope, ":")

	if len(requiredParts) == 2 {
		// short syntax was used <resource>:<verb> which is equal to <resource>::<verb>, or <resource>:*:<verb>
		requiredParts = []string{requiredParts[0], "", requiredParts[1]}
	}

	// Must have same structure (resource:resourceName:verb)
	if len(userParts) != 3 || len(requiredParts) != 3 {
		return false
	}

	// Resource type must match
	if userParts[0] != requiredParts[0] {
		return false
	}

	// Verb must match
	if userParts[2] != "all" && userParts[2] != requiredParts[2] {
		return false
	}

	// Check resource name with wildcard support
	// Empty string or "*" in user scope means all resources
	if userParts[1] == "" || userParts[1] == "*" {
		return true
	}

	// Check resource name with wildcard support
	// Empty string or "*" in required scope means all resources
	if requiredParts[1] == "" || requiredParts[1] == "*" {
		return true
	}

	// Specific resource name must match
	return userParts[1] == requiredParts[1]
}
