package auth

import (
	"crypto/rsa"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims extends standard JWT claims with KDex specific fields.
type Claims struct {
	Email  string   `json:"email"`
	Scopes []string `json:"scopes"`
	UID    string   `json:"uid"`
	jwt.RegisteredClaims
}

// SignToken creates a new signed JWT with the provided user details using RS256.
func SignToken(uid, email string, scopes []string, privateKey *rsa.PrivateKey, duration time.Duration) (string, error) {
	claims := Claims{
		Email:  email,
		Scopes: scopes,
		UID:    uid,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(duration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "kdex-auth-server",
			Subject:   uid,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(privateKey)
}
