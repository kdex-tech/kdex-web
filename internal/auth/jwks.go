package auth

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"

	"kdex.dev/web/internal/keys"
)

// JWK represents a JSON Web Key.
type JWK struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Use string `json:"use"`
	// RSA fields
	E string `json:"e,omitempty"`
	N string `json:"n,omitempty"`
	// ECDSA fields
	Crv string `json:"crv,omitempty"`
	X   string `json:"x,omitempty"`
	Y   string `json:"y,omitempty"`
}

// JWKSet represents a JSON Web Key Set.
type JWKSet struct {
	Keys []JWK `json:"keys"`
}

// JWKSHandler creates an HTTP handler that serves the JWKS endpoint.
// This endpoint exposes the public key(s) used to verify JWT signatures.
func JWKSHandler(keyPairs *keys.KeyPairs) http.HandlerFunc {
	keys := []JWK{}

	for _, pair := range *keyPairs {
		pub := pair.Private.Public()

		// Base JWK info
		item := JWK{
			Use: "sig",
			Kid: pair.KeyId,
		}

		switch v := pub.(type) {
		case *rsa.PublicKey:
			item.Kty = "RSA"
			item.Alg = "RS256"
			item.N = base64.RawURLEncoding.EncodeToString(v.N.Bytes())
			item.E = base64.RawURLEncoding.EncodeToString(big.NewInt(int64(v.E)).Bytes())

		case *ecdsa.PublicKey:
			item.Kty = "EC"
			item.Alg = "ES256"
			item.Crv = v.Params().Name // Usually "P-256"
			// ECDSA requires X and Y coordinates padded to the curve size
			byteSize := (v.Curve.Params().BitSize + 7) / 8
			item.X = base64.RawURLEncoding.EncodeToString(padBytes(v.X.Bytes(), byteSize))
			item.Y = base64.RawURLEncoding.EncodeToString(padBytes(v.Y.Bytes(), byteSize))
		}

		keys = append(keys, item)
	}

	jwks := JWKSet{
		Keys: keys,
	}

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=3600")

		if err := json.NewEncoder(w).Encode(jwks); err != nil {
			http.Error(w, "Failed to encode JWKS", http.StatusInternalServerError)
			return
		}
	}
}

// Helper to ensure ECDSA coordinates are the correct length
func padBytes(src []byte, size int) []byte {
	if len(src) >= size {
		return src
	}
	out := make([]byte, size)
	copy(out[size-len(src):], src)
	return out
}
