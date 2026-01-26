package auth

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/cel-go/cel"
	"golang.org/x/oauth2"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/web/internal"
)

type Config struct {
	ActivePair      *KeyPair
	ClientID        string
	ClientSecret    string
	KeyPairs        *KeyPairs
	MappingRules    []CompiledMappingRule
	OIDCProviderURL string
	RedirectURL     string
	Scopes          []string
	TokenTTL        time.Duration
}

type Exchanger struct {
	Client              client.Client
	Context             context.Context
	ControllerNamespace string
	FocalHost           string
	Provider            *oidc.Provider
	Verifier            *oidc.IDTokenVerifier
	OAuth2Config        *oauth2.Config
	Config              *Config

	rc *RoleController
}

type CompiledMappingRule struct {
	kdexv1alpha1.MappingRule
	Program cel.Program
}

func NewConfig(ctx context.Context, c client.Client, auth *kdexv1alpha1.Auth, namespace string, devMode bool) (*Config, error) {
	cfg := &Config{}

	if auth != nil {
		keyPairs, err := LoadOrGenerateKeyPair(
			ctx,
			c,
			namespace,
			auth.JWT,
			devMode,
		)
		if err != nil {
			return nil, err
		}

		cfg.KeyPairs = keyPairs
		cfg.ActivePair = keyPairs.ActiveKey()

		if cfg.ActivePair != nil {
			ttl, err := time.ParseDuration(auth.JWT.TokenTTL)
			if err != nil {
				return nil, err
			}

			cfg.TokenTTL = ttl
		}

		if auth.OIDCProvider != nil && auth.OIDCProvider.OIDCProviderURL != "" {
			clientSecret, err := LoadClientSecret(ctx, c, namespace, &auth.OIDCProvider.ClientSecretRef)
			if err != nil {
				return nil, err
			}

			mappers, err := compileMappers(auth.OIDCProvider.Mappers)
			if err != nil {
				return nil, err
			}

			cfg.ClientID = auth.OIDCProvider.ClientID
			cfg.ClientSecret = clientSecret
			cfg.MappingRules = mappers
			cfg.OIDCProviderURL = auth.OIDCProvider.OIDCProviderURL
			cfg.RedirectURL = "/~/oauth/callback"
			cfg.Scopes = auth.OIDCProvider.Scopes
		}
	}

	return cfg, nil
}

func (c *Config) AddAuthentication(mux http.Handler) http.Handler {
	if c == nil || c.ActivePair == nil {
		return mux
	}
	return WithAuthentication(c.ActivePair.Private.Public())(mux)
}

func NewExchanger(
	ctx context.Context,
	c client.Client,
	focalHost string,
	controllerNamespace string,
	cfg *Config,
) (*Exchanger, error) {
	ex := &Exchanger{
		Client:              c,
		Config:              cfg,
		Context:             ctx,
		ControllerNamespace: controllerNamespace,
		FocalHost:           focalHost,
	}

	rc, err := NewRoleController(ctx, c, focalHost, controllerNamespace)
	if err != nil {
		return nil, err
	}

	ex.rc = rc

	if cfg.OIDCProviderURL != "" {
		provider, err := oidc.NewProvider(ctx, cfg.OIDCProviderURL)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize OIDC provider: %w", err)
		}
		ex.Provider = provider
		ex.Verifier = provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})

		scopes := []string{oidc.ScopeOpenID, "profile", "email"}
		for _, newScope := range cfg.Scopes {
			if !slices.Contains(scopes, newScope) {
				scopes = append(scopes, newScope)
			}
		}

		ex.OAuth2Config = &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.RedirectURL,
			Scopes:       scopes,
		}
	}

	return ex, nil
}

// AuthCodeURL returns the URL to redirect the user to for OIDC login.
func (e *Exchanger) AuthCodeURL(state string) string {
	if e == nil || e.OAuth2Config == nil {
		return ""
	}
	return e.OAuth2Config.AuthCodeURL(state)
}

// Exchange converts an authorization code into a ID Token.
func (e *Exchanger) ExchangeCode(ctx context.Context, code string) (string, error) {
	if e == nil || e.OAuth2Config == nil {
		return "", fmt.Errorf("OIDC is not configured")
	}

	oauthToken, err := e.OAuth2Config.Exchange(ctx, code)
	if err != nil {
		return "", fmt.Errorf("failed to exchange oauth code")
	}

	// Extract ID Token from oauthToken
	rawIDToken, ok := oauthToken.Extra("id_token").(string)
	if !ok {
		return "", fmt.Errorf("no id_token in response")
	}

	return rawIDToken, nil
}

// VerifyIDToken verifies the raw ID Token and returns the ID Token object.
func (e *Exchanger) VerifyIDToken(ctx context.Context, rawIDToken string) (*oidc.IDToken, error) {
	if e == nil || e.Verifier == nil {
		return nil, fmt.Errorf("OIDC is not configured")
	}
	return e.Verifier.Verify(ctx, rawIDToken)
}

// ExchangeToken verifies a Google OIDC ID Token and exchanges it for a local JWT.
func (e *Exchanger) ExchangeToken(ctx context.Context, rawIDToken string) (string, error) {
	if e == nil {
		return "", fmt.Errorf("exchanger is nil")
	}
	// 1. Verify OIDC Token
	idToken, err := e.VerifyIDToken(ctx, rawIDToken)
	if err != nil {
		return "", fmt.Errorf("failed to verify ID token: %w", err)
	}

	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		return "", fmt.Errorf("failed to parse claims: %w", err)
	}

	email, _ := claims["email"].(string)
	sub, _ := claims["sub"].(string)

	bindings, err := e.resolveRoleBindings(ctx, sub)
	if err != nil {
		return "", err
	}

	roles, err := e.resolveRoles(bindings, sub)
	if err != nil {
		return "", fmt.Errorf("failed to resolve roles: %w", err)
	}

	scopes := e.rc.CollectScopes(roles)

	extra, err := e.MapClaims(e.Config.MappingRules, claims)
	if err != nil {
		return "", fmt.Errorf("failed to map claims: %w", err)
	}

	// 3. Mint Local Token
	return SignToken(sub, email, scopes, extra, e.Config.ActivePair, e.Config.TokenTTL)
}

// LoginLocal authenticates against a Kubernetes Secret and returns a local JWT.
func (e *Exchanger) LoginLocal(ctx context.Context, username, password string) (string, error) {
	if e == nil {
		return "", fmt.Errorf("local auth not configured")
	}

	bindings, err := e.resolveRoleBindings(ctx, username)
	if err != nil {
		return "", err
	}

	passwordValid := false
	email := username
	for _, binding := range bindings.Items {
		if binding.Spec.SecretRef != nil {
			var secret corev1.Secret
			if err := e.Client.Get(ctx, client.ObjectKey{
				Name:      binding.Spec.SecretRef.Name,
				Namespace: binding.Namespace,
			}, &secret); client.IgnoreNotFound(err) != nil {
				return "", fmt.Errorf("failed checking secret for binding %s/%s: %w", binding.Namespace, binding.Name, err)
			}
			passBytes, ok := secret.Data[username]
			if ok && string(passBytes) == password {
				passwordValid = true
				email = binding.Spec.Email
				break
			}
		}
	}

	if passwordValid {
		return "", fmt.Errorf("invalid credentials")
	}

	roles, err := e.resolveRoles(bindings, username)
	if err != nil {
		return "", fmt.Errorf("failed to resolve roles: %w", err)
	}

	scopes := e.rc.CollectScopes(roles)

	// 4. Mint Token
	// Email is dummy or same as username
	return SignToken(username, email, scopes, nil, e.Config.ActivePair, e.Config.TokenTTL)
}

func (e *Exchanger) MapClaims(rules []CompiledMappingRule, rawClaims map[string]any) (map[string]any, error) {
	resultClaims := make(map[string]interface{})

	// The input 'oidc' variable we defined in our CEL env
	input := map[string]interface{}{
		"oidc": rawClaims,
	}

	for _, rule := range rules {
		// 1. Execute the CEL program
		// We use the index 'i' to match the rule to its pre-compiled program
		out, _, err := rule.Program.Eval(input)
		if err != nil {
			return nil, fmt.Errorf("failed to eval expression %q: %w", rule.SourceExpression, err)
		}

		// 2. Convert CEL ref.Val to native Go type (string, bool, map, etc.)
		val, err := out.ConvertToNative(reflect.TypeOf("")) // Assuming string, or use dynamic conversion
		if err != nil {
			// Fallback for complex types like lists/maps
			val = out.Value()
		}

		// 3. Set the value in the target path (e.g., "auth.internal_groups")
		if err := e.setNestedPath(resultClaims, rule.TargetPropPath, val); err != nil {
			return nil, err
		}
	}

	return resultClaims, nil
}

func (e *Exchanger) resolveRoleBindings(ctx context.Context, sub string) (*kdexv1alpha1.KDexRoleBindingList, error) {
	var scopeBindings kdexv1alpha1.KDexRoleBindingList
	if err := e.Client.List(ctx, &scopeBindings, client.InNamespace(e.ControllerNamespace), client.MatchingFields{
		internal.HOST_INDEX_KEY: e.FocalHost,
		internal.SUB_INDEX_KEY:  sub,
	}); err != nil {
		return nil, err
	}
	return &scopeBindings, nil
}

func (e *Exchanger) resolveRoles(list *kdexv1alpha1.KDexRoleBindingList, sub string) ([]string, error) {
	var roles []string
	for _, policy := range list.Items {
		// Generalized sub matching: exact match for now, can be extended to regex
		if policy.Spec.Subject == sub || policy.Spec.Subject == "*" {
			roles = append(roles, policy.Spec.Roles...)
		}
	}
	return roles, nil
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

func compileMappers(rules []kdexv1alpha1.MappingRule) ([]CompiledMappingRule, error) {
	cm := []CompiledMappingRule{}

	env, _ := cel.NewEnv(cel.Variable("oidc", cel.MapType(cel.StringType, cel.AnyType)))

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
