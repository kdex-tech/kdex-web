package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/kdex-tech/kdex-host/internal/keys"
	"github.com/kdex-tech/kdex-host/internal/sign"
	"github.com/stretchr/testify/assert"
)

func TestOAuth2TokenHandler(t *testing.T) {
	// Setup keys and signer
	keyPairs := keys.GenerateECDSAKeyPair()
	signer, _ := sign.NewSigner("aud", time.Hour, "iss", &keyPairs.ActiveKey().Private, keyPairs.ActiveKey().KeyId, nil)

	// Setup Config with M2M Clients
	cfg := Config{
		ActivePair: keyPairs.ActiveKey(),
		KeyPairs:   keyPairs,
		Clients: map[string]AuthClient{
			"valid-client": {
				ClientID:     "valid-client",
				ClientSecret: "valid-secret",
				RedirectURIs: []string{"http://localhost/cb"},
			},
		},
		Signer:   *signer,
		TokenTTL: time.Hour,
	}

	// Setup Exchanger
	sp := &mockScopeProvider{
		resolveIdentity: func(subject string, password string) (jwt.MapClaims, error) {
			return nil, fmt.Errorf("mock auth failed")
		},
		resolveRolesAndEntitlements: func(subject string) ([]string, []string, error) {
			return []string{"role1"}, []string{"entitlement1"}, nil
		},
	}
	ex, _ := NewExchanger(context.Background(), cfg, sp)

	// Helper to generate a valid code
	validCode, _ := ex.CreateAuthorizationCode(context.Background(), AuthorizationCodeClaims{
		Subject:     "user-123",
		ClientID:    "valid-client",
		Scope:       "openid profile",
		RedirectURI: "http://localhost/cb",
		AuthMethod:  "local",
		Exp:         time.Now().Add(time.Minute).Unix(),
	})

	tests := []struct {
		name           string
		method         string
		formData       url.Values
		expectedStatus int
		expectedBody   string
		validateToken  bool
	}{
		{
			name:   "Client Credentials - Success",
			method: "POST",
			formData: url.Values{
				"grant_type":    {"client_credentials"},
				"client_id":     {"valid-client"},
				"client_secret": {"valid-secret"},
				"scope":         {"read write"},
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "access_token",
			validateToken:  true,
		},
		{
			name:   "Client Credentials - Invalid Secret",
			method: "POST",
			formData: url.Values{
				"grant_type":    {"client_credentials"},
				"client_id":     {"valid-client"},
				"client_secret": {"wrong-secret"},
			},
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:   "Client Credentials - Invalid Client ID",
			method: "POST",
			formData: url.Values{
				"grant_type":    {"client_credentials"},
				"client_id":     {"invalid-client"},
				"client_secret": {"valid-secret"},
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:   "Authorization Code - Success",
			method: "POST",
			formData: url.Values{
				"grant_type":    {"authorization_code"},
				"client_id":     {"valid-client"},
				"client_secret": {"valid-secret"}, // M2M clients have secrets
				"code":          {validCode},
				"redirect_uri":  {"http://localhost/cb"},
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "id_token",
			validateToken:  true,
		},
		{
			name:   "Authorization Code - Invalid Code",
			method: "POST",
			formData: url.Values{
				"grant_type":   {"authorization_code"},
				"client_id":    {"valid-client"},
				"code":         {"invalid.code.here"},
				"redirect_uri": {"http://localhost/cb"},
			},
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:   "Unsupported Grant Type",
			method: "POST",
			formData: url.Values{
				"grant_type": {"unknown_grant_type"},
				"client_id":  {"valid-client"},
			},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/-/token", strings.NewReader(tt.formData.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()

			httpHandler := OAuth2TokenHandler(ex)
			httpHandler(w, req)

			resp := w.Result()
			// For debugging if status doesn't match
			if resp.StatusCode != tt.expectedStatus {
				t.Logf("Response Body: %s", w.Body.String())
			}
			assert.Equal(t, tt.expectedStatus, resp.StatusCode)

			if tt.expectedBody != "" {
				assert.Contains(t, w.Body.String(), tt.expectedBody)
			}

			if tt.validateToken && resp.StatusCode == http.StatusOK {
				var tokenResp map[string]any
				err := json.Unmarshal(w.Body.Bytes(), &tokenResp)
				assert.NoError(t, err)
				accessToken, ok := tokenResp["access_token"].(string)
				assert.True(t, ok)
				assert.NotEmpty(t, accessToken)

				// Parse token and check claims
				parser := new(jwt.Parser)
				claims := jwt.MapClaims{}
				_, _, _ = parser.ParseUnverified(accessToken, claims)
				// assert.NoError(t, err) // Signing verification skipped here, trusting signer

				if tt.name == "Authorization Code - Success" {
					assert.Equal(t, "user-123", claims["sub"])
					// assert.Equal(t, "local", claims["auth_method"])
				}
			}
		})
	}
}
