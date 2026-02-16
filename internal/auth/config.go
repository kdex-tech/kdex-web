package auth

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"time"

	"github.com/kdex-tech/dmapper"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/web/internal/keys"
	"kdex.dev/web/internal/sign"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Config struct {
	ActivePair            *keys.KeyPair
	AnonymousEntitlements []string
	BlockKey              string
	ClientID              string
	ClientSecret          string
	CookieName            string
	KeyPairs              *keys.KeyPairs
	OIDCProviderURL       string
	RedirectURL           string
	Scopes                []string
	Signer                sign.Signer
	TokenTTL              time.Duration
}

func NewConfig(
	ctx context.Context,
	c client.Client,
	auth *kdexv1alpha1.Auth,
	issuer string,
	namespace string,
	devMode bool,
) (*Config, error) {
	cfg := &Config{}

	if auth != nil {
		keyPairs, err := keys.LoadOrGenerateKeyPair(
			ctx,
			c,
			namespace,
			auth.JWT,
			devMode,
		)
		if err != nil {
			return nil, err
		}
		if keyPairs == nil || len(*keyPairs) == 0 {
			return nil, fmt.Errorf("no key pairs found")
		}

		cfg.AnonymousEntitlements = auth.AnonymousEntitlements
		cfg.CookieName = auth.JWT.CookieName

		if cfg.CookieName == "" {
			cfg.CookieName = "auth_token"
		}

		cfg.KeyPairs = keyPairs
		cfg.ActivePair = keyPairs.ActiveKey()

		ttlString := "1h"
		if auth.JWT.TokenTTL != "" {
			ttlString = auth.JWT.TokenTTL
		}
		ttl, err := time.ParseDuration(ttlString)
		if err != nil {
			return nil, err
		}
		cfg.TokenTTL = ttl

		var mapper *dmapper.Mapper
		if len(auth.ClaimMappings) > 0 {
			mapper, err = dmapper.NewMapper(auth.ClaimMappings)
			if err != nil {
				return nil, err
			}
		}
		signer, err := sign.NewSigner(
			issuer,
			ttl,
			issuer,
			&cfg.ActivePair.Private,
			cfg.ActivePair.KeyId,
			mapper,
		)
		cfg.Signer = *signer

		if auth.OIDCProvider != nil && auth.OIDCProvider.OIDCProviderURL != "" {
			if auth.OIDCProvider.ClientID == "" {
				return nil, fmt.Errorf("there is no client id configured in spec.auth.oidcProvider.clientID")
			}

			clientSecret, err := LoadValueFromSecret(ctx, c, namespace, &auth.OIDCProvider.ClientSecretRef)
			if err != nil {
				return nil, err
			}

			if clientSecret == "" {
				return nil, fmt.Errorf("there is no Secret containing the OIDC client_secret configured")
			}

			blockKey, err := LoadValueFromSecret(ctx, c, namespace, auth.OIDCProvider.BlockKeySecretRef)
			if err != nil {
				return nil, err
			}

			cfg.BlockKey = getOrGenerate(blockKey)
			cfg.ClientID = auth.OIDCProvider.ClientID
			cfg.ClientSecret = clientSecret
			cfg.OIDCProviderURL = auth.OIDCProvider.OIDCProviderURL
			cfg.RedirectURL = "/-/oauth/callback"
			cfg.Scopes = auth.OIDCProvider.Scopes
		}
	}

	return cfg, nil
}

func (c *Config) AddAuthentication(mux http.Handler) http.Handler {
	if !c.IsAuthEnabled() {
		return mux
	}
	return WithAuthentication(c.ActivePair.Private.Public(), c.CookieName)(mux)
}

func (c *Config) IsAuthEnabled() bool {
	if c == nil || c.ActivePair == nil {
		return false
	}
	return true
}

func (c *Config) IsOIDCEnabled() bool {
	if c == nil || c.ActivePair == nil || c.OIDCProviderURL == "" {
		return false
	}
	return true
}

func getOrGenerate(blockKey string) string {
	if blockKey == "" {
		return rand.Text()
	}
	return blockKey
}
