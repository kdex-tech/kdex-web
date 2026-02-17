package auth

import (
	"context"
	"encoding/json"
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
		Clients: map[string]string{
			"valid-client": "valid-secret",
		},
		Signer:   *signer,
		TokenTTL: time.Hour,
	}

	// Setup Exchanger
	sp := &mockScopeProvider{} // We don't need real scope provider for client_credentials if we just pass scopes through
	ex, _ := NewExchanger(context.Background(), cfg, sp)

	handler := OAuth2TokenHandler(ex)

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
			expectedStatus: http.StatusUnauthorized, // Helper returns error which triggers 401
		},
		{
			name:   "Client Credentials - Invalid Client ID",
			method: "POST",
			formData: url.Values{
				"grant_type":    {"client_credentials"},
				"client_id":     {"invalid-client"},
				"client_secret": {"valid-secret"},
			},
			expectedStatus: http.StatusBadRequest, // IsClientValid check at top of handler
		},
		{
			name:   "Unsupported Grant Type",
			method: "POST",
			formData: url.Values{
				"grant_type": {"authorization_code"},
				"client_id":  {"valid-client"},
			},
			expectedStatus: http.StatusNotImplemented,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/-/token", strings.NewReader(tt.formData.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()

			handler(w, req)

			resp := w.Result()
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
				_, _, err = parser.ParseUnverified(accessToken, claims)
				assert.NoError(t, err)
				assert.Equal(t, "valid-client", claims["sub"])
				assert.Equal(t, "client_credentials", claims["grant_type"])
			}
		})
	}
}
