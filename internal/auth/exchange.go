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

type AuthMethod string

const (
	AuthMethodLocal  AuthMethod = "local"
	AuthMethodOIDC   AuthMethod = "oidc"
	AuthMethodOAuth2 AuthMethod = "oauth2"
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

func (e *Exchanger) ExchangeToken(ctx context.Context, rawIDToken string) (string, error) {
	if e == nil || !e.config.IsOIDCEnabled() {
		return "", fmt.Errorf("OIDC is not configured")
	}

	// 1. Verify OIDC Token
	idToken, err := e.verifyIDToken(ctx, rawIDToken)
	if err != nil {
		return "", fmt.Errorf("failed to verify ID token: %w", err)
	}

	var signingContext jwt.MapClaims
	if err := idToken.Claims(&signingContext); err != nil {
		return "", fmt.Errorf("failed to parse claims: %w", err)
	}

	signingContext["idp"] = "oidc"

	sub, err := signingContext.GetSubject()
	if err != nil {
		return "", fmt.Errorf("no sub in id_token")
	}

	roles, entitlements, err := e.sp.ResolveRolesAndEntitlements(sub)
	if err != nil {
		return "", err
	}

	oidcRoles, _ := signingContext["roles"]
	switch v := oidcRoles.(type) {
	case []string:
		oidcRoles = append(v, roles...)
	case string:
		oidcRoles = append([]string{v}, roles...)
	default:
		oidcRoles = roles
	}
	signingContext["roles"] = oidcRoles

	oidcEntitlements, _ := signingContext["entitlements"]
	switch v := oidcEntitlements.(type) {
	case []string:
		oidcEntitlements = append(v, entitlements...)
	case string:
		oidcEntitlements = append([]string{v}, entitlements...)
	default:
		oidcEntitlements = entitlements
	}
	signingContext["entitlements"] = oidcEntitlements

	// 3. Mint Primary Access Token
	return e.config.Signer.Sign(signingContext)
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

func (e *Exchanger) LoginLocal(ctx context.Context, username, password string, scope string, clientID string, authMethod AuthMethod) (string, string, string, error) {
	if e == nil || !e.config.IsAuthEnabled() {
		return "", "", "", fmt.Errorf("local auth not configured")
	}

	signingContext, err := e.sp.VerifyLocalIdentity(username, password)
	if err != nil {
		return "", "", "", err
	}

	switch authMethod {
	case AuthMethodLocal:
		signingContext["auth_method"] = string(AuthMethodLocal)
	case AuthMethodOAuth2:
		signingContext["auth_method"] = string(AuthMethodOAuth2)
	default:
		return "", "", "", fmt.Errorf("unsupported local login auth method: %s", authMethod)
	}

	// Determine granted scopes and filter claims
	requestedScopes := strings.Split(scope, " ")
	if scope == "" {
		// Default scopes for local login if none requested
		requestedScopes = []string{"email", "entitlements", "openid", "profile", "roles"}
	}

	grantedScopes := []string{}
	hasScope := func(s string) bool {
		return slices.Contains(requestedScopes, s)
	}

	// openid scope
	if hasScope("openid") {
		grantedScopes = append(grantedScopes, "openid")
	}

	// email scope
	if hasScope("email") {
		grantedScopes = append(grantedScopes, "email")
	} else {
		delete(signingContext, "email")
	}

	// profile scope
	if hasScope("profile") {
		grantedScopes = append(grantedScopes, "profile")
	} else {
		delete(signingContext, "family_name")
		delete(signingContext, "given_name")
		delete(signingContext, "middle_name")
		delete(signingContext, "name")
		delete(signingContext, "nickname")
		delete(signingContext, "picture")
		delete(signingContext, "updated_at")
	}

	// entitlements and roles
	if hasScope("entitlements") {
		grantedScopes = append(grantedScopes, "entitlements")
	} else {
		delete(signingContext, "entitlements")
	}
	if hasScope("roles") {
		grantedScopes = append(grantedScopes, "roles")
	} else {
		delete(signingContext, "roles")
	}

	// Map any remaining identity scopes
	if scope, ok := signingContext["scope"].(string); ok && scope != "" {
		for s := range strings.SplitSeq(scope, " ") {
			if !slices.Contains(grantedScopes, s) {
				grantedScopes = append(grantedScopes, s)
			}
		}
	}

	grantedScopeStr := strings.Join(grantedScopes, " ")
	if grantedScopeStr != "" {
		signingContext["scope"] = grantedScopeStr
	}

	accessToken, err := e.config.Signer.Sign(signingContext)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to sign access token: %w", err)
	}

	var idToken string
	if slices.Contains(grantedScopes, "openid") {
		// Clone context for ID Token to avoid mutating the original map which might be used elsewhere or if we add more logic later
		idTokenContext := make(jwt.MapClaims, len(signingContext))
		for k, v := range signingContext {
			idTokenContext[k] = v
		}

		// ID Token Audience must be the Client ID
		idTokenContext["aud"] = clientID

		// ID Token should not contain scope
		delete(idTokenContext, "scope")

		idToken, err = e.config.Signer.Sign(idTokenContext)
		if err != nil {
			return "", "", "", fmt.Errorf("failed to sign id token: %w", err)
		}
	}

	return accessToken, idToken, grantedScopeStr, nil
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
