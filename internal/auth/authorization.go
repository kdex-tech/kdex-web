package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/kdex-tech/entitlements/entitlements"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
)

// AuthorizationChecker validates whether a user has the required permissions.
type AuthorizationChecker struct {
	log logr.Logger
	ec  *entitlements.EntitlementsChecker
}

// NewAuthorizationChecker creates a new authorization checker.
func NewAuthorizationChecker(anonymousEntitlements []string, log logr.Logger) *AuthorizationChecker {
	return &AuthorizationChecker{
		ec:  entitlements.NewEntitlementsChecker(anonymousEntitlements, "", false).WithLogger(log.WithName("entitlements")),
		log: log,
	}
}

func (ac *AuthorizationChecker) CheckAccess(
	ctx context.Context,
	kind string,
	resourceName string,
	kdexreqs []kdexv1alpha1.SecurityRequirement,
) (bool, error) {
	if kind == "" || resourceName == "" {
		return false, fmt.Errorf("kind, resourceName must not be empty")
	}

	// Get claims from context
	claims, hasClaims := GetClaims(ctx)
	if !hasClaims {
		claims = &Claims{}
	}

	userEntitlements := entitlements.Entitlements{}

	if len(claims.Entitlements) > 0 {
		userEntitlements["bearer"] = claims.Entitlements
	}
	if claims.Scope != "" {
		scopes := strings.Split(claims.Scope, " ")
		userEntitlements["oauth2"] = scopes
		userEntitlements["oidc"] = scopes
	}

	requirements := entitlements.Requirements{}
	for _, v := range kdexreqs {
		requirements = append(requirements, v)
	}

	ac.log.V(2).Info("CheckAccess", "claim", claims, "entitlements", userEntitlements, "requirements", requirements)

	return ac.ec.VerifyResourceEntitlements(kind, resourceName, userEntitlements, requirements), nil
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
