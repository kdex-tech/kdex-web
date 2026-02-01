package auth

import (
	"context"
	"crypto"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// ContextKey is a custom type for context keys to avoid collisions.
type ContextKey string

const (
	// ClaimsContextKey is the key used to store the JWT claims in the context.
	ClaimsContextKey ContextKey = "claims"
)

// Claims extends standard JWT claims with KDex specific fields.
type Claims struct {
	jwt.RegisteredClaims

	Name                string           `json:"name"`                            // profile
	GivenName           string           `json:"given_name,omitempty"`            // profile
	FamilyName          string           `json:"family_name,omitempty"`           // profile
	MiddleName          string           `json:"middle_name,omitempty"`           // profile
	Nickname            string           `json:"nickname,omitempty"`              // profile
	PreferredUsername   string           `json:"preferred_username,omitempty"`    // profile
	Profile             string           `json:"profile,omitempty"`               // profile
	Picture             string           `json:"picture,omitempty"`               // profile (URL)
	Website             string           `json:"website,omitempty"`               // profile (URL)
	Email               string           `json:"email"`                           // email
	EmailVerified       bool             `json:"email_verified"`                  // email
	Gender              string           `json:"gender,omitempty"`                // profile
	Birthdate           *jwt.NumericDate `json:"birthdate,omitempty"`             // profile
	Zoneinfo            string           `json:"zoneinfo,omitempty"`              // profile
	Locale              string           `json:"locale,omitempty"`                // profile
	PhoneNumber         string           `json:"phone_number,omitempty"`          // phone
	PhoneNumberVerified bool             `json:"phone_number_verified,omitempty"` // phone
	Address             struct {
		Country       string `json:"country,omitempty"`
		Formatted     string `json:"formatted,omitempty"`
		Locality      string `json:"locality,omitempty"`
		PostalCode    string `json:"postal_code,omitempty"`
		Region        string `json:"region,omitempty"`
		StreetAddress string `json:"street_address,omitempty"`
	} `json:"address"`
	UpdatedAt int `json:"updated_at,omitempty"` // profile

	// Custom claims
	Entitlements []string `json:"entitlements,omitempty"` // custom
	Roles        []string `json:"roles,omitempty"`        // custom
}

// WithAuthentication creates a middleware that validates JWT tokens from the Authorization header.
// It injects the claims into the request context if the token is valid.
// If the Header is present but invalid, it returns 401 Unauthorized.
// If the Header is missing, it proceeds without claims (anonymous access).
func WithAuthentication(publicKey crypto.PublicKey, cookieName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log := logf.FromContext(r.Context())

			authHeader := r.Header.Get("Authorization")
			var tokenString string

			var authSource string
			if authHeader != "" {
				// Expect "Bearer <token>"
				parts := strings.Split(authHeader, " ")
				if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
					http.Error(w, "Invalid Authorization header format", http.StatusUnauthorized)
					return
				}
				tokenString = parts[1]
				authSource = "header"
			} else {
				// Check for cookie
				cookie, err := r.Cookie(cookieName)
				if err != nil {
					// Anonymous access
					next.ServeHTTP(w, r)
					return
				}
				tokenString = cookie.Value
				authSource = "cookie"
			}

			claims := &Claims{}

			token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
				return publicKey, nil
			})

			if err != nil || !token.Valid {
				log.Error(err, "Failed to parse JWT")

				if authSource == "cookie" {
					// Clear the cookie
					http.SetCookie(w, &http.Cookie{
						Name:   cookieName,
						Value:  "",
						Path:   "/",
						MaxAge: -1,
					})
					// Redirect to root
					http.Redirect(w, r, "/", http.StatusSeeOther)
					return
				}

				http.Error(w, "Invalid token", http.StatusUnauthorized)
				return
			}

			// Inject claims into context
			ctx := context.WithValue(r.Context(), ClaimsContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetClaims retrieves the claims from the context.
func GetClaims(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(ClaimsContextKey).(*Claims)
	return claims, ok
}
