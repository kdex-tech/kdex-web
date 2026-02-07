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
	resource string,
	resourceName string,
	kdexreqs []kdexv1alpha1.SecurityRequirement,
) (bool, error) {
	if resource == "" || resourceName == "" {
		return false, fmt.Errorf("resource and resourceName must not be empty")
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

	return ac.ec.VerifyResourceEntitlements(resource, resourceName, userEntitlements, requirements)
}

func (ac *AuthorizationChecker) CalculateRequirements(
	resource string,
	resourceName string,
	kdexreqs []kdexv1alpha1.SecurityRequirement,
) ([]kdexv1alpha1.SecurityRequirement, error) {
	if resource == "" || resourceName == "" {
		return nil, fmt.Errorf("resource and resourceName must not be empty")
	}

	requirements := entitlements.Requirements{}
	for _, v := range kdexreqs {
		requirements = append(requirements, v)
	}

	requirements, err := ac.ec.CalculateResourceRequirements(resource, resourceName, requirements)
	if err != nil {
		return nil, err
	}

	kreq := make([]kdexv1alpha1.SecurityRequirement, len(requirements))
	for _, v := range requirements {
		kreq = append(kreq, v)
	}

	return kreq, nil
}
