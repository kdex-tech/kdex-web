package auth

import (
	"encoding/json"
	"net/http"
)

// OAuth2TokenHandler creates an HTTP handler for the OAuth2 token endpoint.
func OAuth2TokenHandler(exchanger *Exchanger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := r.ParseForm(); err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}

		/*
			grant_type			|Required Parameters											|Optional Parameters
			====================|===============================================================|===================
			authorization_code	|code, redirect_uri, client_id, ?code_verifier, ?client_secret	|state
			password			|username, password, client_id,									|scope
			client_credentials	|client_id, client_secret										|scope
			refresh_token		|refresh_token, client_id, client_secret						|scope
		*/

		clientId := r.FormValue("client_id")

		if !exchanger.IsClientValid(clientId) {
			http.Error(w, "Invalid client_id", http.StatusBadRequest)
			return
		}

		grantType := r.FormValue("grant_type")
		scope := r.FormValue("scope")
		var token string
		var idToken string
		var grantedScope string
		var err error

		switch grantType {
		case "password":
			username := r.FormValue("username")
			password := r.FormValue("password")
			token, idToken, grantedScope, err = exchanger.LoginLocal(r.Context(), username, password, scope, clientId, AuthMethodOAuth2)
		case "authorization_code":
			// TODO: Implement authorization_code exchange once code storage is added
			http.Error(w, "grant_type authorization_code not yet supported for local exchange", http.StatusNotImplemented)
			return
		case "client_credentials":
			clientSecret := r.FormValue("client_secret")
			token, idToken, grantedScope, err = exchanger.LoginClient(r.Context(), clientId, clientSecret, scope)
		case "refresh_token":
			// TODO: Implement refresh_token exchange once refresh token storage is added
			http.Error(w, "grant_type refresh_token not yet supported for local exchange", http.StatusNotImplemented)
			return
		default:
			http.Error(w, "Unsupported grant_type", http.StatusBadRequest)
			return
		}

		if err != nil {
			http.Error(w, "Authentication failed", http.StatusUnauthorized)
			return
		}

		resp := TokenResponse{
			AccessToken: token,
			ExpiresIn:   int(exchanger.GetTokenTTL().Seconds()), // Matching default TTL in seconds
			IDToken:     idToken,
			Scope:       grantedScope,
			TokenType:   "Bearer",
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, "Failed to encode token response", http.StatusInternalServerError)
			return
		}
	}
}

// TokenResponse represents the OAuth2 token response.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	IDToken     string `json:"id_token,omitempty"`
	Scope       string `json:"scope,omitempty"`
	TokenType   string `json:"token_type"`
}
