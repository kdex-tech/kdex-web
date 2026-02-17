package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// ContextKey is a custom type for context keys to avoid collisions.
type ContextKey string

const (
	// authContextKey is the key used to store the JWT claims in the context.
	authContextKey ContextKey = "auth"
)

type AuthContext jwt.MapClaims

// GetAuthContext retrieves the claims from the context.
func GetAuthContext(ctx context.Context) (AuthContext, bool) {
	claims, ok := ctx.Value(authContextKey).(AuthContext)
	return claims, ok
}

// SetAuthContext sets the auth context in the context.
func SetAuthContext(ctx context.Context, ac AuthContext) context.Context {
	return context.WithValue(ctx, authContextKey, ac)
}

// GetExpirationTime implements the Claims interface.
func (ac AuthContext) GetExpirationTime() (*jwt.NumericDate, error) {
	return jwt.MapClaims(ac).GetExpirationTime()
}

// GetNotBefore implements the Claims interface.
func (ac AuthContext) GetNotBefore() (*jwt.NumericDate, error) {
	return jwt.MapClaims(ac).GetNotBefore()
}

// GetIssuedAt implements the Claims interface.
func (ac AuthContext) GetIssuedAt() (*jwt.NumericDate, error) {
	return jwt.MapClaims(ac).GetIssuedAt()
}

// GetAudience implements the Claims interface.
func (ac AuthContext) GetAudience() (jwt.ClaimStrings, error) {
	return jwt.MapClaims(ac).GetAudience()
}

// GetIssuer implements the Claims interface.
func (ac AuthContext) GetIssuer() (string, error) {
	return jwt.MapClaims(ac).GetIssuer()
}

// GetSubject implements the Claims interface.
func (ac AuthContext) GetSubject() (string, error) {
	return jwt.MapClaims(ac).GetSubject()
}

func (ac AuthContext) GetEntitlements() ([]string, error) {
	return ac.parseToStringArray("entitlements")
}

func (ac AuthContext) GetAuthMethod() (AuthMethod, error) {
	authMethod, ok := ac["auth_method"].(string)
	if !ok {
		return "", fmt.Errorf("auth_method is not a string")
	}
	return AuthMethod(authMethod), nil
}

func (ac AuthContext) GetRoles() ([]string, error) {
	return ac.parseToStringArray("roles")
}

func (ac AuthContext) GetScopes() ([]string, error) {
	key := "scope"
	var cs []string
	switch v := ac[key].(type) {
	case string:
		for s := range strings.SplitSeq(v, " ") {
			cs = append(cs, strings.TrimSpace(s))
		}
	case []string:
		for _, s := range v {
			cs = append(cs, strings.TrimSpace(s))
		}
	case []any:
		for _, a := range v {
			vs, ok := a.(string)
			if !ok {
				return nil, fmt.Errorf("%s is invalid", key)
			}
			cs = append(cs, strings.TrimSpace(vs))
		}
	}

	return cs, nil
}

// parseToStringArray tries to parse a key in the map claims type as a
// []string type, which can either be a string, an array of string, or a
// space-separated string.
func (ac AuthContext) parseToStringArray(key string) ([]string, error) {
	var cs []string
	switch v := ac[key].(type) {
	case string:
		cs = append(cs, strings.TrimSpace(v))
	case []string:
		for _, s := range v {
			cs = append(cs, strings.TrimSpace(s))
		}
	case []any:
		for _, a := range v {
			vs, ok := a.(string)
			if !ok {
				return nil, fmt.Errorf("%s is invalid", key)
			}
			cs = append(cs, strings.TrimSpace(vs))
		}
	}

	return cs, nil
}
