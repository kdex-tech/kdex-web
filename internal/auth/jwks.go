package auth

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
)

// JWK represents a JSON Web Key.
type JWK struct {
	Alg string `json:"alg"`
	E   string `json:"e"`
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	N   string `json:"n"`
	Use string `json:"use"`
}

// JWKSet represents a JSON Web Key Set.
type JWKSet struct {
	Keys []JWK `json:"keys"`
}

// JWKSHandler creates an HTTP handler that serves the JWKS endpoint.
// This endpoint exposes the public key(s) used to verify JWT signatures.
func JWKSHandler(publicKey *rsa.PublicKey) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Convert RSA public key to JWK format
		jwk := JWK{
			Kty: "RSA",
			Use: "sig",
			Alg: "RS256",
			Kid: "kdex-auth-key-1",
			N:   base64.RawURLEncoding.EncodeToString(publicKey.N.Bytes()),
			E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(publicKey.E)).Bytes()),
		}

		jwks := JWKSet{
			Keys: []JWK{jwk},
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=3600")

		if err := json.NewEncoder(w).Encode(jwks); err != nil {
			http.Error(w, "Failed to encode JWKS", http.StatusInternalServerError)
			return
		}
	}
}
