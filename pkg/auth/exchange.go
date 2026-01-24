package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
)

type Config struct {
	OIDCProviderURL string
	ClientID        string
	JWTSecret       []byte
	// LocalSecretRef is the reference to the secret containing local users
	LocalSecretRef types.NamespacedName
}

type Exchanger struct {
	Client   client.Client
	Provider *oidc.Provider
	Verifier *oidc.IDTokenVerifier
	Config   Config
}

func NewExchanger(ctx context.Context, c client.Client, cfg Config) (*Exchanger, error) {
	ex := &Exchanger{
		Client: c,
		Config: cfg,
	}

	if cfg.OIDCProviderURL != "" {
		provider, err := oidc.NewProvider(ctx, cfg.OIDCProviderURL)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize OIDC provider: %w", err)
		}
		ex.Provider = provider
		ex.Verifier = provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})
	}

	return ex, nil
}

// ExchangeToken verifies a Google OIDC ID Token and exchanges it for a local JWT.
func (e *Exchanger) ExchangeToken(ctx context.Context, rawIDToken string) (string, error) {
	if e.Verifier == nil {
		return "", fmt.Errorf("OIDC is not configured")
	}

	// 1. Verify OIDC Token
	idToken, err := e.Verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return "", fmt.Errorf("failed to verify ID token: %w", err)
	}

	var claims struct {
		Email string `json:"email"`
		Sub   string `json:"sub"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return "", fmt.Errorf("failed to parse claims: %w", err)
	}

	bindings, err := e.resolveBindings(ctx, claims.Sub)
	if err != nil {
		return "", err
	}

	// 2. Resolve Roles via UserPolicy
	roles, err := e.resolveScopes(bindings, claims.Sub)
	if err != nil {
		return "", fmt.Errorf("failed to resolve roles: %w", err)
	}

	// 3. Mint Local Token
	return SignToken(claims.Sub, claims.Email, roles, e.Config.JWTSecret, 1*time.Hour)
}

// LoginLocal authenticates against a Kubernetes Secret and returns a local JWT.
func (e *Exchanger) LoginLocal(ctx context.Context, username, password string) (string, error) {
	bindings, err := e.resolveBindings(ctx, username)
	if err != nil {
		return "", err
	}

	secret, err := e.resolveSecret(ctx, bindings, username)
	if err != nil {
		return "", err
	}

	// 2. Verify Credentials
	// Expecting secret to have keys matching username and value matching password
	// Or a structured format. For simplicity, we assume key=username, val=password.
	passBytes, ok := secret.Data[username]
	if !ok || string(passBytes) != password {
		return "", fmt.Errorf("invalid credentials")
	}

	// 3. Resolve Roles for local user (pseudo-sub)
	roles, err := e.resolveScopes(bindings, username)
	if err != nil {
		// Fallback: if no policy maps "local:user", maybe give default roles or fail?
		// For now, fail or return empty.
		// Let's assume there must be a policy for them too.
		return "", fmt.Errorf("failed to resolve roles for local user: %w", err)
	}

	// 4. Mint Token
	// Email is dummy or same as username
	return SignToken(username, username, roles, e.Config.JWTSecret, 1*time.Hour)
}

func (e *Exchanger) resolveBindings(ctx context.Context, sub string) (*kdexv1alpha1.KDexScopeBindingList, error) {
	// List all KDexScopeBindings and find match
	// Optimized: Could use FieldIndexer on 'spec.subject' in main.go
	var scopeBindings *kdexv1alpha1.KDexScopeBindingList
	if err := e.Client.List(ctx, scopeBindings); err != nil {
		return nil, err
	}

	return scopeBindings, nil
}

func (e *Exchanger) resolveScopes(list *kdexv1alpha1.KDexScopeBindingList, sub string) ([]string, error) {
	var scopes []string
	found := false
	for _, policy := range list.Items {
		// Generalized sub matching: exact match for now, can be extended to regex
		if policy.Spec.Subject == sub || policy.Spec.Subject == "*" {
			scopes = append(scopes, policy.Spec.Scopes...)
			found = true
		}
	}

	if !found {
		return nil, fmt.Errorf("no binding found for subject %s", sub)
	}

	return scopes, nil
}

func (e *Exchanger) resolveSecret(ctx context.Context, list *kdexv1alpha1.KDexScopeBindingList, sub string) (*corev1.Secret, error) {
	for _, binding := range list.Items {
		if binding.Spec.SecretRef != nil {
			var secret corev1.Secret
			if err := e.Client.Get(ctx, client.ObjectKey{
				Name:      binding.Spec.SecretRef.Name,
				Namespace: binding.Namespace,
			}, &secret); err != nil {
				return nil, fmt.Errorf("failed to get secret for binding %s/%s: %w", binding.Namespace, binding.Name, err)
			}
			return &secret, nil
		}
	}
	return nil, fmt.Errorf("no binding found with secret for subject %s", sub)
}
