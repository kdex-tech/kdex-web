package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims extends standard JWT claims with KDex specific fields.
type Claims struct {
	UID   string   `json:"uid"`
	Email string   `json:"email"`
	Roles []string `json:"roles"`
	jwt.RegisteredClaims
}

// SignToken creates a new signed JWT with the provided user details.
func SignToken(uid, email string, roles []string, secret []byte, duration time.Duration) (string, error) {
	claims := Claims{
		UID:   uid,
		Email: email,
		Roles: roles,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(duration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "kdex-auth-server",
			Subject:   uid,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}
