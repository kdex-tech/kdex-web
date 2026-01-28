package auth

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/go-logr/logr"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
)

// AuthorizationChecker validates whether a user has the required permissions.
type AuthorizationChecker struct {
	anonymousGrants []string
	log             logr.Logger
}

// NewAuthorizationChecker creates a new authorization checker.
func NewAuthorizationChecker(anonymousGrants []string, log logr.Logger) *AuthorizationChecker {
	return &AuthorizationChecker{
		anonymousGrants: anonymousGrants,
		log:             log,
	}
}

func (ac *AuthorizationChecker) CheckAccess(
	ctx context.Context,
	kind string,
	resourceName string,
	original []kdexv1alpha1.SecurityRequirement,
) (bool, error) {
	if kind == "" || resourceName == "" {
		return false, fmt.Errorf("kind, resourceName must not be empty")
	}

	// The identity scope allows for pattern matching
	identity := fmt.Sprintf("%s:%s:read", kind, resourceName)

	// make sure never to write back
	requirements := DeepCloneSecurityRequirements(original)

	// The identity scope is added to all requirements
	added := false
	for _, req := range requirements {
		for i, v := range req {
			if !slices.Contains(v, identity) {
				v = append(v, identity)
				req[i] = v
				added = true
			}
		}
	}
	// when there are no requirements, a fallback wit the identity is added
	if !added {
		requirements = append(requirements, kdexv1alpha1.SecurityRequirement{
			"_": []string{identity},
		})
	}

	// Get claims from context
	claims, hasClaims := GetClaims(ctx)
	if !hasClaims {
		claims = &Claims{}
	}
	// the scopes grated to ananonymous, merge with any existing
	for _, anonGrant := range ac.anonymousGrants {
		if !slices.Contains(claims.Scopes, anonGrant) {
			claims.Scopes = append(claims.Scopes, anonGrant)
		}
	}

	ac.log.V(2).Info("CheckAccess", "claim", claims, "requirements", requirements)

	return ac.validateSecurityRequirements(requirements, claims.Scopes), nil
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

	// Requirements are AND'ed - Check if user has all required scopes
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

func DeepCloneSecurityRequirements(input []kdexv1alpha1.SecurityRequirement) []kdexv1alpha1.SecurityRequirement {
	if input == nil {
		return nil
	}

	// 1. Clone the outer slice
	clone := make([]kdexv1alpha1.SecurityRequirement, len(input))

	for i, reqMap := range input {
		if reqMap == nil {
			continue
		}

		// 2. Clone the Map
		newMap := make(kdexv1alpha1.SecurityRequirement, len(reqMap))
		for key, scopes := range reqMap {
			if scopes == nil {
				newMap[key] = nil
				continue
			}

			// 3. Clone the inner Slice
			newScopes := make([]string, len(scopes))
			copy(newScopes, scopes)
			newMap[key] = newScopes
		}
		clone[i] = newMap
	}

	return clone
}
