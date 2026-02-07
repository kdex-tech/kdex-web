package auth

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/cel-go/cel"
	"golang.org/x/oauth2"

	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
)

type CompiledMappingRule struct {
	kdexv1alpha1.MappingRule
	Program cel.Program
}

type Exchanger struct {
	config       Config
	oauth2Config *oauth2.Config
	oidcProvider *oidc.Provider
	oidcVerifier *oidc.IDTokenVerifier
	sp           ScopeProvider
}

func NewExchanger(
	ctx context.Context,
	cfg Config,
	sp ScopeProvider,
) (*Exchanger, error) {
	ex := &Exchanger{
		config: cfg,
		sp:     sp,
	}

	if cfg.IsOIDCEnabled() {
		provider, err := oidc.NewProvider(ctx, cfg.OIDCProviderURL)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize OIDC provider: %w", err)
		}
		ex.oidcProvider = provider
		ex.oidcVerifier = provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})

		scopes := []string{oidc.ScopeOpenID, "profile", "email"}
		for _, newScope := range cfg.Scopes {
			if !slices.Contains(scopes, newScope) {
				scopes = append(scopes, newScope)
			}
		}

		ex.oauth2Config = &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.RedirectURL,
			Scopes:       scopes,
		}
	}

	return ex, nil
}

func (e *Exchanger) AuthCodeURL(state string) string {
	if e == nil || !e.config.IsOIDCEnabled() {
		return ""
	}
	return e.oauth2Config.AuthCodeURL(state)
}

func (e *Exchanger) EndSessionURL() (string, error) {
	if e == nil || !e.config.IsOIDCEnabled() {
		return "", nil
	}
	var claims OIDCProviderClaims
	if err := e.oidcProvider.Claims(&claims); err != nil {
		return "", err
	}
	return claims.EndSessionURL, nil
}

func (e *Exchanger) ExchangeCode(ctx context.Context, code string) (string, error) {
	if e == nil || !e.config.IsOIDCEnabled() {
		return "", fmt.Errorf("OIDC is not configured")
	}

	oauthToken, err := e.oauth2Config.Exchange(ctx, code)
	if err != nil {
		return "", fmt.Errorf("failed to exchange oauth code %w", err)
	}

	// Extract ID Token from oauthToken
	rawIDToken, ok := oauthToken.Extra("id_token").(string)
	if !ok {
		return "", fmt.Errorf("no id_token in response")
	}

	return rawIDToken, nil
}

func (e *Exchanger) ExchangeToken(ctx context.Context, issuer string, rawIDToken string) (string, error) {
	if e == nil || !e.config.IsOIDCEnabled() {
		return "", fmt.Errorf("OIDC is not configured")
	}
	// 1. Verify OIDC Token
	idToken, err := e.verifyIDToken(ctx, rawIDToken)
	if err != nil {
		return "", fmt.Errorf("failed to verify ID token: %w", err)
	}

	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		return "", fmt.Errorf("failed to parse claims: %w", err)
	}

	email, _ := claims["email"].(string)
	sub, _ := claims["sub"].(string)

	roles, entitlements, err := e.sp.ResolveRolesAndEntitlements(sub)
	if err != nil {
		return "", err
	}

	extra, err := e.MapClaims(e.config.MappingRules, claims)
	if err != nil {
		return "", fmt.Errorf("failed to map claims: %w", err)
	}

	// 2. Add OIDC standard profile claims if they exist
	for _, k := range []string{"family_name", "given_name", "middle_name", "name", "nickname", "picture", "updated_at"} {
		if v, ok := claims[k]; ok {
			extra[k] = v
		}
	}

	extra["roles"] = roles
	extra["entitlements"] = entitlements

	// 3. Mint Local Token
	return SignToken(sub, email, e.config.ClientID, issuer, extra, e.config.ActivePair, e.config.TokenTTL)
}

func (e *Exchanger) GetClientID() string {
	return e.config.ClientID
}

func (e *Exchanger) GetTokenTTL() time.Duration {
	return e.config.TokenTTL
}

func (e *Exchanger) GetScopesSupported() ([]string, error) {
	if e == nil || !e.config.IsOIDCEnabled() {
		return nil, nil
	}
	var claims OIDCProviderClaims
	if err := e.oidcProvider.Claims(&claims); err != nil {
		return nil, err
	}
	return claims.ScopesSupported, nil
}

func (e *Exchanger) LoginLocal(ctx context.Context, issuer string, username, password string, scope string) (string, string, error) {
	if e == nil || !e.config.IsAuthEnabled() {
		return "", "", fmt.Errorf("local auth not configured")
	}

	identity, err := e.sp.VerifyLocalIdentity(username, password)
	if err != nil {
		return "", "", err
	}

	extra, err := e.MapClaims(e.config.MappingRules, identity.Extra)
	if err != nil {
		return "", "", fmt.Errorf("failed to map claims: %w", err)
	}

	// Determine granted scopes and filter claims
	requestedScopes := strings.Split(scope, " ")
	if scope == "" {
		// Default scopes for local login if none requested
		requestedScopes = []string{"email", "roles", "entitlements"}
	}

	grantedScopes := []string{}
	hasScope := func(s string) bool {
		return slices.Contains(requestedScopes, s)
	}

	// email scope
	if hasScope("email") {
		extra["email"] = identity.Email
		grantedScopes = append(grantedScopes, "email")
	}

	// profile scope
	if hasScope("profile") {
		grantedScopes = append(grantedScopes, "profile")
		if identity.FamilyName != "" {
			extra["family_name"] = identity.FamilyName
		}
		if identity.GivenName != "" {
			extra["given_name"] = identity.GivenName
		}
		if identity.MiddleName != "" {
			extra["middle_name"] = identity.MiddleName
		}
		if identity.Name != "" {
			extra["name"] = identity.Name
		}
		if identity.Nickname != "" {
			extra["nickname"] = identity.Nickname
		}
		if identity.Picture != "" {
			extra["picture"] = identity.Picture
		}
		if identity.UpdatedAt != 0 {
			extra["updated_at"] = identity.UpdatedAt
		}
	}

	// entitlements and roles
	if hasScope("entitlements") && len(identity.Entitlements) > 0 {
		extra["entitlements"] = identity.Entitlements
		grantedScopes = append(grantedScopes, "entitlements")
	}
	if hasScope("roles") && len(identity.Roles) > 0 {
		extra["roles"] = identity.Roles
		grantedScopes = append(grantedScopes, "roles")
	}

	// Map any remaining identity scopes
	if identity.Scope != "" {
		extra["scope"] = identity.Scope
	}

	grantedScopeStr := strings.Join(grantedScopes, " ")
	if grantedScopeStr != "" {
		extra["scope"] = grantedScopeStr
	}

	token, err := SignToken(username, identity.Email, e.config.ClientID, issuer, extra, e.config.ActivePair, e.config.TokenTTL)
	return token, grantedScopeStr, err
}

func (e *Exchanger) MapClaims(rules []CompiledMappingRule, rawClaims map[string]any) (map[string]any, error) {
	resultClaims := make(map[string]any)

	// The input 'token' variable we defined in our CEL env
	input := map[string]any{
		"token": rawClaims,
	}

	for _, rule := range rules {
		// 1. Execute the CEL program
		out, _, err := rule.Program.Eval(input)
		if err != nil {
			if !rule.Required {
				continue
			}

			return nil, fmt.Errorf("failed to eval expression %q: %w", rule.SourceExpression, err)
		}

		// 2. Convert CEL ref.Val to native Go type (string, bool, map, etc.)
		val, err := out.ConvertToNative(reflect.TypeFor[string]()) // Assuming string, or use dynamic conversion
		if err != nil {
			// Fallback for complex types like lists/maps
			val = out.Value()
		}

		// 3. Set the value in the target path (e.g., "auth.internal_groups")
		if err := e.setNestedPath(resultClaims, rule.TargetPropPath, val); err != nil {
			if !rule.Required {
				continue
			}

			return nil, err
		}
	}

	return resultClaims, nil
}

func (e *Exchanger) setNestedPath(m map[string]any, path string, value any) error {
	parts := strings.Split(path, ".")
	current := m

	for i, part := range parts {
		if i == len(parts)-1 {
			current[part] = value
			return nil
		}

		if _, exists := current[part]; !exists {
			current[part] = make(map[string]any)
		}

		next, ok := current[part].(map[string]any)
		if !ok {
			return fmt.Errorf("path conflict at %s", part)
		}
		current = next
	}
	return nil
}

func (e *Exchanger) verifyIDToken(ctx context.Context, rawIDToken string) (*oidc.IDToken, error) {
	if e == nil || !e.config.IsOIDCEnabled() {
		return nil, fmt.Errorf("OIDC is not configured")
	}
	return e.oidcVerifier.Verify(ctx, rawIDToken)
}

type OIDCProviderClaims struct {
	EndSessionURL   string   `json:"end_session_endpoint"`
	ScopesSupported []string `json:"scopes_supported"`
}

func compileMappers(rules []kdexv1alpha1.MappingRule) ([]CompiledMappingRule, error) {
	cm := []CompiledMappingRule{}

	env, _ := cel.NewEnv(cel.Variable("token", cel.MapType(cel.StringType, cel.AnyType)))

	for _, rule := range rules {
		ast, issues := env.Compile(rule.SourceExpression)
		if issues.Err() != nil {
			return nil, issues.Err()
		}
		prog, err := env.Program(ast)
		if err != nil {
			return nil, err
		}
		cm = append(cm, CompiledMappingRule{
			MappingRule: rule,
			Program:     prog,
		})
	}

	return cm, nil
}
