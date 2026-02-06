package auth

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kdex.dev/crds/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type IH struct {
	http.HandlerFunc
	Handler http.HandlerFunc
}

func (f *IH) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.Handler(w, r)
}

func MockOIDCProvider(cfg Config) http.HandlerFunc {
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		// Use server.URL to get the actual assigned port/address
		issuer := cfg.OIDCProviderURL

		config := map[string]any{
			"authorization_endpoint":                issuer + "/auth",
			"end_session_endpoint":                  issuer + "/logout",
			"id_token_signing_alg_values_supported": []string{"ES256", "RS256"},
			"issuer":                                issuer,
			"jwks_uri":                              issuer + "/jwks.json",
			"response_types_supported":              []string{"code", "id_token"},
			"subject_types_supported":               []string{"public"},
			"token_endpoint":                        issuer + "/token",
			"userinfo_endpoint":                     issuer + "/userinfo",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(config)
	})

	mux.HandleFunc("/jwks.json", JWKSHandler(cfg.KeyPairs))
	mux.HandleFunc("POST /token", TokenHandler(cfg))
	mux.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		// 1. Validate the id_token_hint (optional for mocks)
		// 2. Redirect back to the post_logout_redirect_uri
		redirectURI := r.URL.Query().Get("post_logout_redirect_uri")
		if redirectURI == "" {
			redirectURI = "/"
		}
		http.Redirect(w, r, redirectURI, http.StatusFound)
	})
	return mux.ServeHTTP
}

func MockRunningServer(innerHandler *IH) *httptest.Server {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		innerHandler.ServeHTTP(w, r)
	})

	return httptest.NewServer(handler)
}

func TestNewExchanger(t *testing.T) {
	extra := map[string]any{
		"firstName": "Joe",
		"lastName":  "Bar",
		"address": map[string]any{
			"street":  "1 Long Dr",
			"city":    "Baytown",
			"country": "Supernautica",
		},
	}
	scopeProvider := &mockScopeProvider{
		verifyLocalIdentity: func(subject string, password string) (*Identity, error) {
			if subject == "not-allowed" {
				return nil, fmt.Errorf("invalid credentials")
			}

			return &Identity{
				Email:        subject,
				Extra:        extra,
				Subject:      subject,
				Entitlements: []string{"foo", "bar"},
			}, nil
		},
		resolveRolesAndEntitlements: func(subject string) ([]string, []string, error) {
			return nil, []string{"page:read"}, nil
		},
	}

	tests := []struct {
		name       string
		cfg        Config
		sp         ScopeProvider
		assertions func(t *testing.T, got *Exchanger, goterr error)
	}{
		{
			name: "constructor",
			sp:   scopeProvider,
			assertions: func(t *testing.T, got *Exchanger, goterr error) {
				assert.NotNil(t, got)
				assert.Nil(t, goterr)
			},
		},
		{
			name: "AuthCodeURL when there is no OIDC provider",
			sp:   scopeProvider,
			assertions: func(t *testing.T, got *Exchanger, goterr error) {
				assert.NotNil(t, got)
				url := got.AuthCodeURL("foo")
				assert.Equal(t, "", url)
			},
		},
		{
			name: "ExchangeCode when there is no OIDC provider",
			sp:   scopeProvider,
			assertions: func(t *testing.T, got *Exchanger, goterr error) {
				assert.NotNil(t, got)
				_, err := got.ExchangeCode(context.Background(), "foo")
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), "OIDC is not configured")
			},
		},
		{
			name: "VerifyIDToken when there is no OIDC provider",
			sp:   scopeProvider,
			assertions: func(t *testing.T, got *Exchanger, goterr error) {
				assert.NotNil(t, got)
				_, err := got.verifyIDToken(context.Background(), "foo")
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), "OIDC is not configured")
			},
		},
		{
			name: "ExchangeToken when there is no OIDC provider",
			sp:   scopeProvider,
			assertions: func(t *testing.T, got *Exchanger, goterr error) {
				assert.NotNil(t, got)
				_, err := got.ExchangeToken(context.Background(), "issuer", "foo")
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), "OIDC is not configured")
			},
		},
		{
			name: "LoginLocal when there is no auth.Config",
			sp:   scopeProvider,
			assertions: func(t *testing.T, got *Exchanger, goterr error) {
				assert.NotNil(t, got)
				token, _, err := got.LoginLocal(context.Background(), "issuer", "not-allowed", "password", "")
				assert.Equal(t, "", token)
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), "local auth not configured")
			},
		},
		{
			name: "LoginLocal invalid subject",
			cfg: func() Config {
				c, _ := NewConfig(context.Background(), nil, &v1alpha1.Auth{}, "foo", true)
				return *c
			}(),
			sp: scopeProvider,
			assertions: func(t *testing.T, got *Exchanger, goterr error) {
				assert.NotNil(t, got)
				token, _, err := got.LoginLocal(context.Background(), "issuer", "not-allowed", "password", "")
				assert.Equal(t, "", token)
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), "invalid credentials")
			},
		},
		{
			name: "LoginLocal valid subject",
			cfg: func() Config {
				c, _ := NewConfig(context.Background(), nil, &v1alpha1.Auth{}, "foo", true)
				return *c
			}(),
			sp: scopeProvider,
			assertions: func(t *testing.T, got *Exchanger, goterr error) {
				assert.NotNil(t, got)
				token, _, err := got.LoginLocal(context.Background(), "issuer", "joe", "password", "")
				assert.True(t, len(token) > 100)
				assert.Nil(t, err)
			},
		},
		{
			name: "Mapping rules - simple",
			cfg: func() Config {
				c, _ := NewConfig(context.Background(), nil, &v1alpha1.Auth{
					Mappers: []v1alpha1.MappingRule{
						{
							SourceExpression: "token.address",
							TargetPropPath:   "address",
						},
					},
				}, "foo", true)
				return *c
			}(),
			sp: scopeProvider,
			assertions: func(t *testing.T, got *Exchanger, goterr error) {
				assert.NotNil(t, got)
				token, _, err := got.LoginLocal(context.Background(), "issuer", "joe", "password", "")
				assert.True(t, len(token) > 100)
				assert.Nil(t, err)

				claims := jwt.MapClaims{}
				parser := new(jwt.Parser)
				jwtToken, _, err := parser.ParseUnverified(token, claims)
				assert.Nil(t, err)
				assert.NotNil(t, jwtToken)
				assert.Contains(t, jwtToken.Header["kid"], "kdex-dev-")
				assert.NotNil(t, claims["address"])
				assert.Equal(t, "1 Long Dr", claims["address"].(map[string]any)["street"])
				assert.Equal(t, "Baytown", claims["address"].(map[string]any)["city"])
			},
		},
		{
			name: "Mapping rules - required, but fails",
			cfg: func() Config {
				c, _ := NewConfig(context.Background(), nil, &v1alpha1.Auth{
					Mappers: []v1alpha1.MappingRule{
						{
							Required:         true,
							SourceExpression: "token.job",
							TargetPropPath:   "job",
						},
					},
				}, "foo", true)
				return *c
			}(),
			sp: scopeProvider,
			assertions: func(t *testing.T, got *Exchanger, goterr error) {
				assert.NotNil(t, got)
				_, _, err := got.LoginLocal(context.Background(), "issuer", "joe", "password", "")
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), "failed to map claims: failed to eval expression")
			},
		},
		{
			name: "Mapping rules - required, success",
			cfg: func() Config {
				c, _ := NewConfig(context.Background(), nil, &v1alpha1.Auth{
					Mappers: []v1alpha1.MappingRule{
						{
							Required:         true,
							SourceExpression: "token.address.street",
							TargetPropPath:   "street",
						},
						{
							SourceExpression: "token.job",
							TargetPropPath:   "job",
						},
					},
				}, "foo", true)
				return *c
			}(),
			sp: scopeProvider,
			assertions: func(t *testing.T, got *Exchanger, goterr error) {
				assert.NotNil(t, got)
				token, _, err := got.LoginLocal(context.Background(), "issuer", "joe", "password", "")
				assert.True(t, len(token) > 100)
				assert.Nil(t, err)

				claims := jwt.MapClaims{}
				parser := new(jwt.Parser)
				jwtToken, _, err := parser.ParseUnverified(token, claims)
				assert.Nil(t, err)
				assert.NotNil(t, jwtToken)
				assert.Contains(t, jwtToken.Header["kid"], "kdex-dev-")
				assert.Equal(t, "1 Long Dr", claims["street"])
				assert.Nil(t, claims["job"])
			},
		},
		{
			name: "Mapping rules - deeply nest",
			cfg: func() Config {
				c, _ := NewConfig(context.Background(), nil, &v1alpha1.Auth{
					Mappers: []v1alpha1.MappingRule{
						{
							Required:         true,
							SourceExpression: "token.address.street",
							TargetPropPath:   "other.place.street",
						},
					},
				}, "foo", true)
				return *c
			}(),
			sp: scopeProvider,
			assertions: func(t *testing.T, got *Exchanger, goterr error) {
				assert.NotNil(t, got)
				token, _, err := got.LoginLocal(context.Background(), "issuer", "joe", "password", "")
				assert.True(t, len(token) > 100)
				assert.Nil(t, err)

				claims := jwt.MapClaims{}
				parser := new(jwt.Parser)
				jwtToken, _, err := parser.ParseUnverified(token, claims)
				assert.Nil(t, err)
				assert.NotNil(t, jwtToken)
				assert.Contains(t, jwtToken.Header["kid"], "kdex-dev-")
				assert.Equal(t, "1 Long Dr", claims["other"].(map[string]any)["place"].(map[string]any)["street"])
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			got, gotErr := NewExchanger(ctx, tt.cfg, tt.sp)
			tt.assertions(t, got, gotErr)
		})
	}
}

func TestNewExchanger_OIDC(t *testing.T) {
	extra := map[string]any{
		"firstName": "Joe",
		"lastName":  "Bar",
		"address": map[string]any{
			"street":  "1 Long Dr",
			"city":    "Baytown",
			"country": "Supernautica",
		},
	}
	scopeProvider := &mockScopeProvider{
		verifyLocalIdentity: func(subject string, password string) (*Identity, error) {
			if subject == "not-allowed" {
				return nil, fmt.Errorf("invalid credentials")
			}

			return &Identity{
				Email:        subject,
				Extra:        extra,
				Subject:      subject,
				Entitlements: []string{"foo", "bar"},
			}, nil
		},
		resolveRolesAndEntitlements: func(subject string) ([]string, []string, error) {
			return nil, []string{"page:read"}, nil
		},
	}

	tests := []struct {
		name       string
		cfg        func(string) (Config, error)
		sp         ScopeProvider
		assertions func(t *testing.T, serverURL string, innerHandler *IH)
	}{
		{
			name: "OIDC - constructor, bad provider url",
			sp:   scopeProvider,
			assertions: func(t *testing.T, serverURL string, innerHandler *IH) {
				ctx := context.Background()
				client := fake.NewClientBuilder().WithObjects(
					&v1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "foo",
						},
						Data: map[string][]byte{
							"client_secret": []byte("bar"),
						},
					},
				).Build()
				cfg, gotErr := NewConfig(ctx, client, &v1alpha1.Auth{
					OIDCProvider: &v1alpha1.OIDCProvider{
						ClientID: "foo",
						ClientSecretRef: v1alpha1.LocalSecretWithKeyReference{
							KeyProperty: "client_secret",
							SecretRef: v1.LocalObjectReference{
								Name: "foo",
							},
						},
						OIDCProviderURL: "http://bad",
					},
				}, "foo", true)
				assert.Nil(t, gotErr)
				assert.Equal(t, "bar", cfg.ClientSecret)

				innerHandler.Handler = MockOIDCProvider(*cfg)
				_, gotErr = NewExchanger(ctx, *cfg, scopeProvider)
				assert.NotNil(t, gotErr)
				assert.Contains(t, gotErr.Error(), `failed to initialize OIDC provider: Get "http://bad/.well-known/openid-configuration"`)
			},
		},
		{
			name: "OIDC - constructor, good provider url",
			sp:   scopeProvider,
			assertions: func(t *testing.T, serverURL string, innerHandler *IH) {
				ctx := context.Background()
				client := fake.NewClientBuilder().WithObjects(
					&v1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "foo",
						},
						Data: map[string][]byte{
							"client_secret": []byte("bar"),
						},
					},
				).Build()
				cfg, gotErr := NewConfig(ctx, client, &v1alpha1.Auth{
					OIDCProvider: &v1alpha1.OIDCProvider{
						ClientID: "foo",
						ClientSecretRef: v1alpha1.LocalSecretWithKeyReference{
							KeyProperty: "client_secret",
							SecretRef: v1.LocalObjectReference{
								Name: "foo",
							},
						},
						OIDCProviderURL: serverURL,
					},
				}, "foo", true)
				assert.Nil(t, gotErr)
				assert.Equal(t, "bar", cfg.ClientSecret)

				innerHandler.Handler = MockOIDCProvider(*cfg)
				_, gotErr = NewExchanger(ctx, *cfg, scopeProvider)
				assert.Nil(t, gotErr)
			},
		},
		{
			name: "OIDC - AuthCodeURL",
			sp:   scopeProvider,
			assertions: func(t *testing.T, serverURL string, innerHandler *IH) {
				ctx := context.Background()
				client := fake.NewClientBuilder().WithObjects(
					&v1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "foo",
						},
						Data: map[string][]byte{
							"client_secret": []byte("bar"),
						},
					},
				).Build()
				cfg, gotErr := NewConfig(ctx, client, &v1alpha1.Auth{
					OIDCProvider: &v1alpha1.OIDCProvider{
						ClientID: "foo",
						ClientSecretRef: v1alpha1.LocalSecretWithKeyReference{
							KeyProperty: "client_secret",
							SecretRef: v1.LocalObjectReference{
								Name: "foo",
							},
						},
						OIDCProviderURL: serverURL,
					},
				}, "foo", true)
				assert.Nil(t, gotErr)
				assert.Equal(t, "bar", cfg.ClientSecret)

				innerHandler.Handler = MockOIDCProvider(*cfg)
				ex, gotErr := NewExchanger(ctx, *cfg, scopeProvider)
				assert.Nil(t, gotErr)
				url := ex.AuthCodeURL("foo")
				assert.Contains(t, url, "http://", "client_id=foo", "scope=openid+profile+email", "state=foo")
			},
		},
		{
			name: "OIDC - AuthCodeURL, extra scopes",
			sp:   scopeProvider,
			assertions: func(t *testing.T, serverURL string, innerHandler *IH) {
				ctx := context.Background()
				client := fake.NewClientBuilder().WithObjects(
					&v1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "foo",
						},
						Data: map[string][]byte{
							"client_secret": []byte("bar"),
						},
					},
				).Build()
				cfg, gotErr := NewConfig(ctx, client, &v1alpha1.Auth{
					OIDCProvider: &v1alpha1.OIDCProvider{
						ClientID: "foo",
						ClientSecretRef: v1alpha1.LocalSecretWithKeyReference{
							KeyProperty: "client_secret",
							SecretRef: v1.LocalObjectReference{
								Name: "foo",
							},
						},
						OIDCProviderURL: serverURL,
						Scopes:          []string{"job"},
					},
				}, "foo", true)
				assert.Nil(t, gotErr)
				assert.Equal(t, "bar", cfg.ClientSecret)

				innerHandler.Handler = MockOIDCProvider(*cfg)
				ex, gotErr := NewExchanger(ctx, *cfg, scopeProvider)
				assert.Nil(t, gotErr)
				url := ex.AuthCodeURL("foo")
				assert.Contains(t, url, "http://", "client_id=foo", "scope=openid+profile+email+job", "state=foo")
			},
		},
		{
			name: "OIDC - ExchangeCode",
			sp:   scopeProvider,
			assertions: func(t *testing.T, serverURL string, innerHandler *IH) {
				ctx := context.Background()
				client := fake.NewClientBuilder().WithObjects(
					&v1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "foo",
						},
						Data: map[string][]byte{
							"client_secret": []byte("bar"),
						},
					},
				).Build()
				cfg, gotErr := NewConfig(ctx, client, &v1alpha1.Auth{
					OIDCProvider: &v1alpha1.OIDCProvider{
						ClientID: "foo",
						ClientSecretRef: v1alpha1.LocalSecretWithKeyReference{
							KeyProperty: "client_secret",
							SecretRef: v1.LocalObjectReference{
								Name: "foo",
							},
						},
						OIDCProviderURL: serverURL,
					},
				}, "foo", true)
				assert.Nil(t, gotErr)
				assert.Equal(t, "bar", cfg.ClientSecret)

				innerHandler.Handler = MockOIDCProvider(*cfg)
				ex, gotErr := NewExchanger(ctx, *cfg, scopeProvider)
				assert.Nil(t, gotErr)
				rawIDToken, err := ex.ExchangeCode(ctx, "foo")
				claims := jwt.MapClaims{}
				parser := new(jwt.Parser)
				jwtToken, _, err := parser.ParseUnverified(rawIDToken, claims)
				assert.Nil(t, err)
				assert.NotNil(t, jwtToken)
				assert.Contains(t, jwtToken.Header["kid"], "kdex-dev-")
				iss, err := claims.GetIssuer()
				assert.Nil(t, err)
				assert.Equal(t, cfg.OIDCProviderURL, iss)
			},
		},
		{
			name: "OIDC - ExchangeCode, failed exchange",
			sp:   scopeProvider,
			assertions: func(t *testing.T, serverURL string, innerHandler *IH) {
				ctx := context.Background()
				client := fake.NewClientBuilder().WithObjects(
					&v1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "foo",
						},
						Data: map[string][]byte{
							"client_secret": []byte("bar"),
						},
					},
				).Build()
				cfg, gotErr := NewConfig(ctx, client, &v1alpha1.Auth{
					OIDCProvider: &v1alpha1.OIDCProvider{
						ClientID: "foo",
						ClientSecretRef: v1alpha1.LocalSecretWithKeyReference{
							KeyProperty: "client_secret",
							SecretRef: v1.LocalObjectReference{
								Name: "foo",
							},
						},
						OIDCProviderURL: serverURL,
					},
				}, "foo", true)
				assert.Nil(t, gotErr)
				assert.Equal(t, "bar", cfg.ClientSecret)

				innerHandler.Handler = MockOIDCProvider(*cfg)
				ex, gotErr := NewExchanger(ctx, *cfg, scopeProvider)
				assert.Nil(t, gotErr)
				_, err := ex.ExchangeCode(ctx, "fail_exchange")
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), "Internal Server Error")
			},
		},
		{
			name: "OIDC - ExchangeCode, id token missing",
			sp:   scopeProvider,
			assertions: func(t *testing.T, serverURL string, innerHandler *IH) {
				ctx := context.Background()
				client := fake.NewClientBuilder().WithObjects(
					&v1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "foo",
						},
						Data: map[string][]byte{
							"client_secret": []byte("bar"),
						},
					},
				).Build()
				cfg, gotErr := NewConfig(ctx, client, &v1alpha1.Auth{
					OIDCProvider: &v1alpha1.OIDCProvider{
						ClientID: "foo",
						ClientSecretRef: v1alpha1.LocalSecretWithKeyReference{
							KeyProperty: "client_secret",
							SecretRef: v1.LocalObjectReference{
								Name: "foo",
							},
						},
						OIDCProviderURL: serverURL,
					},
				}, "foo", true)
				assert.Nil(t, gotErr)
				assert.Equal(t, "bar", cfg.ClientSecret)

				innerHandler.Handler = MockOIDCProvider(*cfg)
				ex, gotErr := NewExchanger(ctx, *cfg, scopeProvider)
				assert.Nil(t, gotErr)
				_, err := ex.ExchangeCode(ctx, "no_id_token")
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), "no id_token in response")
			},
		},
		{
			name: "OIDC - VerifyIDToken",
			sp:   scopeProvider,
			assertions: func(t *testing.T, serverURL string, innerHandler *IH) {
				ctx := context.Background()
				client := fake.NewClientBuilder().WithObjects(
					&v1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "foo",
						},
						Data: map[string][]byte{
							"client_secret": []byte("bar"),
						},
					},
				).Build()
				cfg, gotErr := NewConfig(ctx, client, &v1alpha1.Auth{
					OIDCProvider: &v1alpha1.OIDCProvider{
						ClientID: "foo",
						ClientSecretRef: v1alpha1.LocalSecretWithKeyReference{
							KeyProperty: "client_secret",
							SecretRef: v1.LocalObjectReference{
								Name: "foo",
							},
						},
						OIDCProviderURL: serverURL,
					},
				}, "foo", true)
				assert.Nil(t, gotErr)
				assert.Equal(t, "bar", cfg.ClientSecret)

				innerHandler.Handler = MockOIDCProvider(*cfg)
				ex, gotErr := NewExchanger(ctx, *cfg, scopeProvider)
				assert.Nil(t, gotErr)
				rawIDToken, err := ex.ExchangeCode(ctx, "foo")
				assert.Nil(t, err)
				oidcToken, err := ex.verifyIDToken(ctx, rawIDToken)
				assert.Nil(t, err)
				assert.NotNil(t, oidcToken)
				assert.Equal(t, cfg.ClientID, oidcToken.Audience[0])
			},
		},
		{
			name: "OIDC - ExchangeToken",
			sp:   scopeProvider,
			assertions: func(t *testing.T, serverURL string, innerHandler *IH) {
				ctx := context.Background()
				client := fake.NewClientBuilder().WithObjects(
					&v1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "foo",
						},
						Data: map[string][]byte{
							"client_secret": []byte("bar"),
						},
					},
				).Build()
				cfg, gotErr := NewConfig(ctx, client, &v1alpha1.Auth{
					OIDCProvider: &v1alpha1.OIDCProvider{
						ClientID: "foo",
						ClientSecretRef: v1alpha1.LocalSecretWithKeyReference{
							KeyProperty: "client_secret",
							SecretRef: v1.LocalObjectReference{
								Name: "foo",
							},
						},
						OIDCProviderURL: serverURL,
					},
				}, "foo", true)
				assert.Nil(t, gotErr)
				assert.Equal(t, "bar", cfg.ClientSecret)

				innerHandler.Handler = MockOIDCProvider(*cfg)
				ex, gotErr := NewExchanger(ctx, *cfg, scopeProvider)
				assert.Nil(t, gotErr)
				rawIDToken, err := ex.ExchangeCode(ctx, "foo")
				assert.Nil(t, err)
				strinToken, err := ex.ExchangeToken(ctx, cfg.OIDCProviderURL, rawIDToken)
				assert.Nil(t, err)
				claims := jwt.MapClaims{}
				parser := new(jwt.Parser)
				jwtToken, _, err := parser.ParseUnverified(strinToken, claims)
				assert.Nil(t, err)
				assert.NotNil(t, jwtToken)
				assert.Contains(t, jwtToken.Header["kid"], "kdex-dev-")
				iss, err := claims.GetIssuer()
				assert.Nil(t, err)
				assert.Equal(t, cfg.OIDCProviderURL, iss)
				entitlements := []string{}
				for _, s := range claims["entitlements"].([]any) {
					entitlements = append(entitlements, s.(string))
				}
				assert.Equal(t, []string{"page:read"}, entitlements)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ih := &IH{}
			server := MockRunningServer(ih)
			defer server.Close()
			tt.assertions(t, server.URL, ih)
		})
	}
}

func TokenHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 0. OIDC Token requests are almost always POST with form-encoded data
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form data", http.StatusBadRequest)
			return
		}

		grantType := r.FormValue("grant_type")
		code := r.FormValue("code")

		if code == "fail_exchange" {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		clientID := r.FormValue("client_id")

		if clientID == "" {
			username, _, ok := r.BasicAuth()
			if ok {
				clientID = username
			}
		}

		// 1. Validation: In a mock, you might just check it's not empty
		if clientID == "" {
			http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
			return
		}

		// 2. Validate the Grant Type
		if grantType != "authorization_code" {
			http.Error(w, `{"error":"unsupported_grant_type"}`, http.StatusBadRequest)
			return
		}

		// 3. Simple Mock Validation
		// In a real mock, you'd check if 'code' exists in a map from the /auth step.
		if code == "" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}

		scheme := r.URL.Scheme
		if scheme == "" {
			scheme = "http"
		}
		issuer := fmt.Sprintf("%s://%s", scheme, r.Host)

		// 4. Generate the ID Token (using your SignToken function)
		// We usually include 'aud' (client_id) and 'sub' (user id)
		idToken, err := SignToken(code, "email@foo.bar", clientID, issuer, nil, cfg.ActivePair, 5*time.Minute)
		if err != nil {
			http.Error(w, "failed to sign token", http.StatusInternalServerError)
			return
		}

		// 5. Construct the Response
		resp := map[string]any{
			"access_token": "mock-access-token-" + rand.Text(),
			"token_type":   "Bearer",
			"expires_in":   3600,
			"scope":        "openid email",
		}

		if code != "no_id_token" {
			resp["id_token"] = idToken
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

type mockScopeProvider struct {
	resolveIdentity             func(subject string, password string, identity *Identity) error
	resolveRolesAndEntitlements func(subject string) ([]string, []string, error)
	verifyLocalIdentity         func(subject string, password string) (*Identity, error)
}

func (m *mockScopeProvider) ResolveIdentity(subject string, password string, identity *Identity) error {
	return m.resolveIdentity(subject, password, identity)
}

func (m *mockScopeProvider) ResolveRolesAndEntitlements(subject string) ([]string, []string, error) {
	return m.resolveRolesAndEntitlements(subject)
}

func (m *mockScopeProvider) VerifyLocalIdentity(subject string, password string) (*Identity, error) {
	return m.verifyLocalIdentity(subject, password)
}
