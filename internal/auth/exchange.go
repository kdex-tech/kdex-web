package auth

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/cel-go/cel"
	"github.com/kdex-tech/dmapper"
	"golang.org/x/oauth2"
)

type CompiledMappingRule struct {
	dmapper.MappingRule
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

	var oidcClaims jwt.MapClaims
	if err := idToken.Claims(&oidcClaims); err != nil {
		return "", fmt.Errorf("failed to parse claims: %w", err)
	}

	sub, err := oidcClaims.GetSubject()
	if err != nil {
		return "", fmt.Errorf("no sub in id_token")
	}

	roles, entitlements, err := e.sp.ResolveRolesAndEntitlements(sub)
	if err != nil {
		return "", err
	}

	oidcRoles, _ := oidcClaims["roles"]
	switch oidcRoles.(type) {
	case []string:
		oidcRoles = append(oidcRoles.([]string), roles...)
	case string:
		oidcRoles = append([]string{oidcRoles.(string)}, roles...)
	default:
		oidcClaims["roles"] = roles
	}

	oidcEntitlements, _ := oidcClaims["entitlements"]
	switch oidcEntitlements.(type) {
	case []string:
		oidcEntitlements = append(oidcEntitlements.([]string), entitlements...)
	case string:
		oidcEntitlements = append([]string{oidcEntitlements.(string)}, entitlements...)
	default:
		oidcClaims["entitlements"] = entitlements
	}

	// 3. Mint Primary Access Token
	return e.config.Signer.Sign(oidcClaims)
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

	signingContext := jwt.MapClaims{}

	identity, err := e.sp.VerifyLocalIdentity(username, password)
	if err != nil {
		return "", "", err
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
		signingContext["email"] = identity.Email
		grantedScopes = append(grantedScopes, "email")
	}

	// profile scope
	if hasScope("profile") {
		grantedScopes = append(grantedScopes, "profile")
		if identity.FamilyName != "" {
			signingContext["family_name"] = identity.FamilyName
		}
		if identity.GivenName != "" {
			signingContext["given_name"] = identity.GivenName
		}
		if identity.MiddleName != "" {
			signingContext["middle_name"] = identity.MiddleName
		}
		if identity.Name != "" {
			signingContext["name"] = identity.Name
		}
		if identity.Nickname != "" {
			signingContext["nickname"] = identity.Nickname
		}
		if identity.Picture != "" {
			signingContext["picture"] = identity.Picture
		}
		if identity.UpdatedAt != 0 {
			signingContext["updated_at"] = identity.UpdatedAt
		}
	}

	// entitlements and roles
	if hasScope("entitlements") && len(identity.Entitlements) > 0 {
		signingContext["entitlements"] = identity.Entitlements
		grantedScopes = append(grantedScopes, "entitlements")
	}
	if hasScope("roles") && len(identity.Roles) > 0 {
		signingContext["roles"] = identity.Roles
		grantedScopes = append(grantedScopes, "roles")
	}

	// Map any remaining identity scopes
	if identity.Scope != "" {
		for s := range strings.SplitSeq(identity.Scope, " ") {
			if !slices.Contains(grantedScopes, s) {
				grantedScopes = append(grantedScopes, s)
			}
		}
	}

	grantedScopeStr := strings.Join(grantedScopes, " ")
	if grantedScopeStr != "" {
		signingContext["scope"] = grantedScopeStr
	}

	token, err := e.config.Signer.Sign(signingContext)
	return token, grantedScopeStr, err
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
