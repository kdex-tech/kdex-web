package auth

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"maps"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// SignToken creates a new signed JWT with the provided user details.
func SignToken(uid, email, audience, issuer string, scopes []string, extra map[string]any, kp *KeyPair, duration time.Duration) (string, error) {
	claims := map[string]any{
		"sub":    uid,
		"uid":    uid,
		"aud":    audience,
		"email":  email,
		"scopes": scopes,
		"iss":    issuer,
		"exp":    jwt.NewNumericDate(time.Now().Add(duration)),
		"iat":    jwt.NewNumericDate(time.Now()),
		"jti":    rand.Text(),
	}

	maps.Copy(claims, extra)

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

	token := jwt.NewWithClaims(method, jwt.MapClaims(claims))
	token.Header["kid"] = kp.KeyId
	return token.SignedString(kp.Private)
}
