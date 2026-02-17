package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiscoveryHandler(t *testing.T) {
	handler := DiscoveryHandler("http://example.com")
	req := httptest.NewRequest("GET", "/.well-known/openid-configuration", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var config OpenIDConfiguration
	err := json.Unmarshal(w.Body.Bytes(), &config)
	assert.NoError(t, err)

	assert.Contains(t, config.GrantTypesSupported, "client_credentials")
	assert.Contains(t, config.GrantTypesSupported, "password")
	assert.Equal(t, "http://example.com", config.Issuer)
}
