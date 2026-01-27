package auth

import (
	"context"
	"fmt"
	"net/http"
	"time"

	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Config struct {
	ActivePair      *KeyPair
	ClientID        string
	ClientSecret    string
	CookieName      string
	KeyPairs        *KeyPairs
	MappingRules    []CompiledMappingRule
	OIDCProviderURL string
	RedirectURL     string
	Scopes          []string
	TokenTTL        time.Duration
}

func NewConfig(ctx context.Context, c client.Client, auth *kdexv1alpha1.Auth, namespace string, devMode bool) (*Config, error) {
	cfg := &Config{}

	if auth != nil {
		cfg.CookieName = auth.JWT.CookieName

		if cfg.CookieName == "" {
			cfg.CookieName = "auth_token"
		}

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
			ttlString := auth.JWT.TokenTTL
			if ttlString == "" {
				ttlString = "1h"
			}
			ttl, err := time.ParseDuration(ttlString)
			if err != nil {
				return nil, err
			}

			cfg.TokenTTL = ttl
		}

		mappers, err := compileMappers(auth.Mappers)
		if err != nil {
			return nil, err
		}
		cfg.MappingRules = mappers

		if auth.OIDCProvider != nil && auth.OIDCProvider.OIDCProviderURL != "" {
			if auth.OIDCProvider.ClientID == "" {
				return nil, fmt.Errorf("there is no client id configured in spec.auth.oidcProvider.clientID")
			}

			clientSecret, err := LoadClientSecret(ctx, c, namespace, &auth.OIDCProvider.ClientSecretRef)
			if err != nil {
				return nil, err
			}

			cfg.ClientID = auth.OIDCProvider.ClientID
			cfg.ClientSecret = clientSecret
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
	return WithAuthentication(c.ActivePair.Private.Public(), c.CookieName)(mux)
}
