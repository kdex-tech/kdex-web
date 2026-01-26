package auth

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims extends standard JWT claims with KDex specific fields.
type Claims struct {
	Email  string   `json:"email"`
	Scopes []string `json:"scopes"`
	UID    string   `json:"uid"`
	jwt.RegisteredClaims
	Extra `json:",inline"`
}

type Extra map[string]any

// SignToken creates a new signed JWT with the provided user details using RS256.
func SignToken(uid, email string, scopes []string, extra map[string]any, kp *KeyPair, duration time.Duration) (string, error) {
	claims := Claims{
		Email:  email,
		Extra:  extra,
		Scopes: scopes,
		UID:    uid,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(duration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "kdex-auth-server",
			Subject:   uid,
		},
	}

	var method jwt.SigningMethod

	// Check the public key type to decide the signing algorithm
	switch kp.Private.Public().(type) {
	case *rsa.PublicKey:
		method = jwt.SigningMethodRS256
	case *ecdsa.PublicKey:
		method = jwt.SigningMethodES256
	default:
		return "", fmt.Errorf("unsupported signer type")
	}

	token := jwt.NewWithClaims(method, claims)
	return token.SignedString(kp.Private)
}
