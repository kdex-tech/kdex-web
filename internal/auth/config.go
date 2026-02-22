package auth

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/kdex-tech/dmapper"
	"github.com/kdex-tech/kdex-host/internal/keys"
	"github.com/kdex-tech/kdex-host/internal/sign"
	corev1 "k8s.io/api/core/v1"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type AuthClient struct {
	AllowedGrantTypes []string
	AllowedScopes     []string
	ClientID          string
	ClientSecret      string
	Description       string
	Name              string
	Public            bool
	RedirectURIs      []string
	RequirePKCE       bool
}

type Config struct {
	ActivePair            *keys.KeyPair
	AnonymousEntitlements []string
	BlockKey              string
	Clients               map[string]AuthClient
	CookieName            string
	KeyPairs              *keys.KeyPairs
	OIDC                  struct {
		ClientID     string
		ClientSecret string
		ProviderURL  string
		RedirectURL  string
		Scopes       []string
	}
	Signer   sign.Signer
	TokenTTL time.Duration
}

func NewConfig(
	ctx context.Context,
	c client.Client,
	auth *kdexv1alpha1.Auth,
	audience string,
	issuer string,
	namespace string,
	devMode bool,
	secrets kdexv1alpha1.ServiceAccountSecrets,
) (*Config, error) {
	cfg := &Config{}

	if auth != nil {
		jwtSecrets := secrets.Filter(func(s corev1.Secret) bool { return s.Annotations["kdex.dev/secret-type"] == "jwt-keys" })
		keyPairs, err := keys.LoadOrGenerateKeyPair(
			ctx,
			c,
			namespace,
			jwtSecrets,
			1, // default 1 hour if not specified
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
			audience,
			ttl,
			issuer,
			&cfg.ActivePair.Private,
			cfg.ActivePair.KeyId,
			mapper,
		)
		cfg.Signer = *signer

		authClientSecrets := secrets.Filter(func(s corev1.Secret) bool { return s.Annotations["kdex.dev/secret-type"] == "auth-client" })
		if len(authClientSecrets) > 0 {
			cfg.Clients = make(map[string]AuthClient)
			for _, secret := range authClientSecrets {
				clientID := string(secret.Data["client_id"])
				if clientID == "" {
					clientID = string(secret.Data["client-id"])
				}

				clientSecret := string(secret.Data["client_secret"])
				if clientSecret == "" {
					clientSecret = string(secret.Data["client-secret"])
				}

				public := false
				if string(secret.Data["public"]) == "true" {
					public = true
				}

				if !public && clientSecret == "" {
					return nil, fmt.Errorf("client %s is not public but has no client secret", clientID)
				}

				redirectURIsStr := string(secret.Data["redirect_uris"])
				if redirectURIsStr == "" {
					redirectURIsStr = string(secret.Data["redirect-uris"])
				}

				redirectURIs := []string{}
				if redirectURIsStr != "" {
					redirectURIs = strings.Split(redirectURIsStr, ",")
				}

				allowedGrantTypesStr := string(secret.Data["allowed_grant_types"])
				if allowedGrantTypesStr == "" {
					allowedGrantTypesStr = string(secret.Data["allowed-grant-types"])
				}
				allowedGrantTypes := []string{}
				if allowedGrantTypesStr != "" {
					allowedGrantTypes = strings.Split(allowedGrantTypesStr, ",")
				}

				allowedScopesStr := string(secret.Data["allowed_scopes"])
				if allowedScopesStr == "" {
					allowedScopesStr = string(secret.Data["allowed-scopes"])
				}
				allowedScopes := []string{}
				if allowedScopesStr != "" {
					allowedScopes = strings.Split(allowedScopesStr, ",")
				}

				description := string(secret.Data["description"])
				name := string(secret.Data["name"])

				requirePKCE := false
				if string(secret.Data["require_pkce"]) == "true" || string(secret.Data["require-pkce"]) == "true" {
					requirePKCE = true
				}

				client := AuthClient{
					AllowedGrantTypes: allowedGrantTypes,
					AllowedScopes:     allowedScopes,
					ClientID:          clientID,
					ClientSecret:      clientSecret,
					Description:       description,
					Name:              name,
					Public:            public,
					RedirectURIs:      redirectURIs,
					RequirePKCE:       requirePKCE,
				}

				cfg.Clients[clientID] = client
			}
		}

		if auth.OIDCProvider != nil && auth.OIDCProvider.OIDCProviderURL != "" {
			oidcSecrets := secrets.Filter(func(s corev1.Secret) bool { return s.Annotations["kdex.dev/secret-type"] == "oidc-client" })
			if len(oidcSecrets) == 0 {
				return nil, fmt.Errorf("missing secret of type 'oidc-client' required for OIDC provider")
			}
			// Use the first one found?
			oidcSecret := oidcSecrets[0]

			// Expect "client-secret" and "block-key" in the secret?
			// Previous model: ClientSecretRef (keyProperty), BlockKeySecretRef (keyProperty).
			// Now we should standardize.
			// Let's assume standard keys: "client_secret" (or "client-secret") and "block_key" (or "block-key").
			// Or maybe "clientSecret" and "blockKey".
			// Let's check keys usage in existing code or standards.
			// "client-id" and "client-secret" are common.

			clientSecret := string(oidcSecret.Data["client_secret"])
			if clientSecret == "" {
				clientSecret = string(oidcSecret.Data["client-secret"])
			}

			if clientSecret == "" {
				return nil, fmt.Errorf("OIDC secret does not contain 'client_secret' or 'client-secret'")
			}

			clientID := string(oidcSecret.Data["client_id"])
			if clientID == "" {
				clientID = string(oidcSecret.Data["client-id"])
			}

			if clientID == "" {
				return nil, fmt.Errorf("OIDC secret does not contain 'client_id' or 'client-id'")
			}

			blockKey := string(oidcSecret.Data["block_key"])
			if blockKey == "" {
				blockKey = string(oidcSecret.Data["block-key"])
			}

			if blockKey == "" && !devMode {
				return nil, fmt.Errorf("a 'block_key' or 'block-key' was not found in the OIDC secret, generating a new one is not supported in production")
			}

			cfg.BlockKey = getOrGenerate(blockKey)

			cfg.OIDC.ClientID = clientID
			cfg.OIDC.ClientSecret = clientSecret
			cfg.OIDC.ProviderURL = auth.OIDCProvider.OIDCProviderURL
			cfg.OIDC.RedirectURL = "/-/oauth/callback"
			cfg.OIDC.Scopes = auth.OIDCProvider.Scopes
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
	if c == nil || c.OIDC.ProviderURL == "" {
		return false
	}
	return true
}

func (c *Config) IsM2MEnabled() bool {
	if c == nil || c.ActivePair == nil || len(c.Clients) == 0 {
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
