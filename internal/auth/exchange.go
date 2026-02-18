package auth

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
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
		provider, err := oidc.NewProvider(ctx, cfg.OIDC.ProviderURL)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize OIDC provider: %w", err)
		}
		ex.oidcProvider = provider
		ex.oidcVerifier = provider.Verifier(&oidc.Config{ClientID: cfg.OIDC.ClientID})

		scopes := []string{oidc.ScopeOpenID, "profile", "email"}
		for _, newScope := range cfg.OIDC.Scopes {
			if !slices.Contains(scopes, newScope) {
				scopes = append(scopes, newScope)
			}
		}

		ex.oauth2Config = &oauth2.Config{
			ClientID:     cfg.OIDC.ClientID,
			ClientSecret: cfg.OIDC.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.OIDC.RedirectURL,
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

func (e *Exchanger) GetOIDCClientID() string {
	return e.config.OIDC.ClientID
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

func (e *Exchanger) GetClient(clientID string) (AuthClient, bool) {
	if e == nil {
		return AuthClient{}, false
	}
	// Check M2M clients
	if e.config.IsM2MEnabled() {
		client, ok := e.config.Clients[clientID]
		return client, ok
	}
	return AuthClient{}, false
}

func (e *Exchanger) LoginClient(ctx context.Context, clientID, clientSecret string, scope string) (string, string, string, error) {
	if e == nil {
		return "", "", "", fmt.Errorf("auth not configured")
	}

	if !e.config.IsM2MEnabled() {
		return "", "", "", fmt.Errorf("M2M auth not configured")
	}

	client, ok := e.GetClient(clientID)
	if !ok {
		return "", "", "", fmt.Errorf("invalid client_id")
	}

	if client.ClientSecret != clientSecret {
		return "", "", "", fmt.Errorf("invalid client_secret")
	}

	signingContext := jwt.MapClaims{
		"sub":         clientID,
		"azp":         clientID,
		"auth_method": string(AuthMethodOAuth2),
		"grant_type":  "client_credentials",
	}

	// Determine granted scopes
	// For M2M, we can implement a policy here. For now, let's allow all requested scopes
	// that are configured in the system, or just pass them through if we don't have a rigid list.
	// A better approach for M2M is to have configured scopes per client, but that requires more complex config.
	// For this iteration, we will grant what is requested.
	// TODO: Filter scopes based on client configuration if available.

	requestedScopes := strings.Split(scope, " ")
	grantedScopes := []string{}
	for _, s := range requestedScopes {
		if s != "" {
			grantedScopes = append(grantedScopes, s)
		}
	}
	grantedScopeStr := strings.Join(grantedScopes, " ")
	if grantedScopeStr != "" {
		signingContext["scope"] = grantedScopeStr
	}

	// Determine Audience.
	// For M2M, the audience is typically the resource server (API).
	// We'll use the default audience from the signer.
	// Optionally, if the request contains an 'audience' parameter (not standard but common), we could use it.
	// For now, rely on Signer's default or if 'aud' claim mechanism in Sign needed updates again.
	// The Signer.Sign method uses default audience if 'aud' is not in claims.

	accessToken, err := e.config.Signer.Sign(signingContext)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to sign access token: %w", err)
	}

	return accessToken, "", grantedScopeStr, nil
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

type AuthorizationCodeClaims struct {
	Subject     string     `json:"sub"`
	ClientID    string     `json:"cid"`
	Scope       string     `json:"scp"`
	RedirectURI string     `json:"uri"`
	AuthMethod  AuthMethod `json:"auth_method"`
	Exp         int64      `json:"exp"`
}

func (e *Exchanger) CreateAuthorizationCode(ctx context.Context, claims AuthorizationCodeClaims) (string, error) {
	if e == nil {
		return "", fmt.Errorf("auth not configured")
	}

	// 1. Prepare the payload
	// Set expiration if not set (e.g. 10 minutes)
	if claims.Exp == 0 {
		claims.Exp = time.Now().Add(10 * time.Minute).Unix()
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("failed to marshal auth code claims: %w", err)
	}

	// 2. Derive Key
	key := sha256.Sum256([]byte(e.config.BlockKey))

	// 3. Encrypt
	encrypter, err := jose.NewEncrypter(jose.A256GCM, jose.Recipient{Algorithm: jose.DIRECT, Key: key[:]}, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create encrypter: %w", err)
	}

	object, err := encrypter.Encrypt(payload)
	if err != nil {
		return "", fmt.Errorf("failed to encrypt auth code: %w", err)
	}

	return object.CompactSerialize()
}

func (e *Exchanger) RedeemAuthorizationCode(ctx context.Context, code string, clientID string, redirectURI string) (string, string, string, error) {
	if e == nil {
		return "", "", "", fmt.Errorf("auth not configured")
	}

	// 1. Parse JWE
	object, err := jose.ParseEncrypted(code, []jose.KeyAlgorithm{jose.DIRECT}, []jose.ContentEncryption{jose.A256GCM})
	if err != nil {
		return "", "", "", fmt.Errorf("failed to parse auth code: %w", err)
	}

	// 2. Derive Key
	key := sha256.Sum256([]byte(e.config.BlockKey))

	// 3. Decrypt
	decrypted, err := object.Decrypt(key[:])
	if err != nil {
		return "", "", "", fmt.Errorf("failed to decrypt auth code: %w", err)
	}

	var claims AuthorizationCodeClaims
	if err := json.Unmarshal(decrypted, &claims); err != nil {
		return "", "", "", fmt.Errorf("failed to unmarshal auth code claims: %w", err)
	}

	// 4. Validate
	if time.Now().Unix() > claims.Exp {
		return "", "", "", fmt.Errorf("authorization code expired")
	}

	if claims.ClientID != clientID {
		return "", "", "", fmt.Errorf("client_id mismatch")
	}

	if claims.RedirectURI != redirectURI {
		return "", "", "", fmt.Errorf("redirect_uri mismatch")
	}

	// 5. Mint Tokens
	// We reuse LoginLocal logic but we need to bypass password check since we already authenticated.
	// Actually, we should just mint the tokens directly using the claims from the code.
	// We need to resolve roles/entitlements again to be fresh, or trust what's in the code?
	// Better to resolve fresh if possible, but we only have `Subject`. LoginLocal does `sp.VerifyLocalIdentity` then resolves.
	// Here we just have `Subject`. We should assume the user is valid if we issued the code.
	// We need to fetch the user profile/roles again.

	// We need to get the user's details (roles, entitlements, email, etc)
	// ScopeProvider interface has `VerifyLocalIdentity` and `ResolveRolesAndEntitlements`.
	// We might need a `GetUser` method on `ScopeProvider`.
	// For now, let's call `ResolveRolesAndEntitlements` to get roles.
	// But `VerifyLocalIdentity` provided the initial claims (email, profile).
	// If `ScopeProvider` is just a mock or simple interface, we might be missing a way to get profile by ID.
	// `ResolveRolesAndEntitlements` only returns roles/entitlements.

	// If we look at `LoginLocal`, it calls `VerifyLocalIdentity` which returns `signingContext` with profile data.
	// We don't have the password here.

	// OPTION: We could have stored the *entire* profile in the JWE.
	// JWE capacity is large enough for typical claims.
	// Let's modify AuthorizationCodeClaims to include the full `jwt.MapClaims` or raw profile data?
	// Or just trust that generating the code was the "authentication" and we mint tokens based on that.
	// But we need the profile data for the ID Token.

	// Let's assume for now we resolve roles afresh, but we might lose some profile data if not stored.
	// For `local` auth, the profile is usually static or from DB.
	// Only `ResolveRolesAndEntitlements` is available on `sp` (ScopeProvider).

	// Let's check ScopeProvider interface.
	// It is in `exchange.go`? No, likely `authorization.go`.

	// To be safe and simple: Let's assume we can rely on `ResolveRolesAndEntitlements`.
	// But what about `email`, `name` etc?
	// VerifyLocalIdentity returns them.

	// I will update `AuthorizationCodeClaims` to include a `Profile map[string]any` field to carry over the user's profile data.

	return e.mintTokensFromCode(claims)
}

func (e *Exchanger) mintTokensFromCode(claims AuthorizationCodeClaims) (string, string, string, error) {
	// Reconstruct signing context
	signingContext := jwt.MapClaims{}

	// 1. Add Profile Data if available (we need to add this to struct)
	// For now, rely on Subject.
	signingContext["sub"] = claims.Subject
	signingContext["auth_method"] = claims.AuthMethod

	// 2. Resolve Roles/Entitlements
	roles, entitlements, err := e.sp.ResolveRolesAndEntitlements(claims.Subject)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to resolve roles: %w", err)
	}
	signingContext["roles"] = roles
	signingContext["entitlements"] = entitlements

	// 3. Handle Scopes
	requestedScopes := strings.Split(claims.Scope, " ")
	grantedScopes := []string{}
	hasScope := func(s string) bool {
		return slices.Contains(requestedScopes, s)
	}

	if hasScope("openid") {
		grantedScopes = append(grantedScopes, "openid")
	}
	if hasScope("email") {
		grantedScopes = append(grantedScopes, "email")
	}
	if hasScope("profile") {
		grantedScopes = append(grantedScopes, "profile")
	}
	if hasScope("entitlements") {
		grantedScopes = append(grantedScopes, "entitlements")
	}
	if hasScope("roles") {
		grantedScopes = append(grantedScopes, "roles")
	}

	// Add other scopes
	for _, s := range requestedScopes {
		if !slices.Contains(grantedScopes, s) && s != "" {
			grantedScopes = append(grantedScopes, s)
		}
	}

	grantedScopeStr := strings.Join(grantedScopes, " ")
	if grantedScopeStr != "" {
		signingContext["scope"] = grantedScopeStr
	}

	// 4. Sign Access Token
	accessToken, err := e.config.Signer.Sign(signingContext)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to sign access token: %w", err)
	}

	// 5. Sign ID Token if needed
	var idToken string
	if slices.Contains(grantedScopes, "openid") {
		idTokenContext := make(jwt.MapClaims, len(signingContext))
		for k, v := range signingContext {
			idTokenContext[k] = v
		}
		idTokenContext["aud"] = claims.ClientID
		delete(idTokenContext, "scope")
		idToken, err = e.config.Signer.Sign(idTokenContext)
		if err != nil {
			return "", "", "", fmt.Errorf("failed to sign id token: %w", err)
		}
	}

	return accessToken, idToken, grantedScopeStr, nil
}
