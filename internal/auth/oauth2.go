package auth

import (
	"encoding/json"
	"fmt"
	"net/http"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// OAuth2TokenHandler creates an HTTP handler for the OAuth2 token endpoint.
func OAuth2TokenHandler(exchanger *Exchanger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var clientId, clientSecret, code, idToken, grantedScope, grantType, password, redirectURI, scope, token, username string
		var err error

		log := logf.FromContext(r.Context())
		defer func() {
			log.Info(
				"OAuth2 token exchange",
				"client_id", clientId,
				"client_secret", clientSecret,
				"code", code,
				"id_token", idToken,
				"grant_type", grantType,
				"password", password,
				"redirect_uri", redirectURI,
				"scope", scope,
				"username", username,
				"error", err)
		}()

		if r.Method != http.MethodPost {
			err = fmt.Errorf("method not allowed")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err = r.ParseForm(); err != nil {
			err = fmt.Errorf("failed to parse form: %w", err)
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}

		// client_id and client_secret may arrive through basic auth
		clientId, clientSecret, _ = r.BasicAuth()

		/*
			grant_type			|Client Type	|Required Parameters								|Optional Parameters
			====================|===============|===================================================|===================
			authorization_code	|Private		|code, redirect_uri, client_id, client_secret		|state
								|Public			|code, redirect_uri, client_id, code_verifier		|state
			client_credentials	|Private		|client_id, client_secret							|scope
			password			|Private		|username, password, client_id, client_secret		|scope
								|Public			|username, password, client_id						|scope
			refresh_token		|Private		|refresh_token, client_id, client_secret			|scope
								|Public			|refresh_token, client_id							|scope
		*/

		if clientId == "" {
			clientId = r.FormValue("client_id")
		}

		client, ok := exchanger.GetClient(clientId)

		if !ok {
			err = fmt.Errorf("invalid client_id")
			http.Error(w, "Invalid client_id", http.StatusBadRequest)
			return
		}

		if !client.Public {
			if clientSecret == "" {
				clientSecret = r.FormValue("client_secret")
			}
			if clientSecret != client.ClientSecret {
				err = fmt.Errorf("invalid client_secret")
				http.Error(w, "Invalid client_secret", http.StatusBadRequest)
				return
			}
		}

		grantType = r.FormValue("grant_type")
		scope = r.FormValue("scope")

		switch grantType {
		case "authorization_code":
			code = r.FormValue("code")
			if code == "" {
				err = fmt.Errorf("code is required")
				http.Error(w, "code is required", http.StatusBadRequest)
				return
			}
			redirectURI = r.FormValue("redirect_uri")
			if redirectURI == "" {
				err = fmt.Errorf("redirect_uri is required")
				http.Error(w, "redirect_uri is required", http.StatusBadRequest)
				return
			}
			token, idToken, grantedScope, err = exchanger.RedeemAuthorizationCode(r.Context(), code, clientId, redirectURI)
		case "client_credentials":
			if client.Public {
				err = fmt.Errorf("client_credentials grant_type is not supported for public clients")
				http.Error(w, "client_credentials grant_type is not supported for public clients", http.StatusBadRequest)
				return
			}
			token, idToken, grantedScope, err = exchanger.LoginClient(r.Context(), clientId, clientSecret, scope)
		case "password":
			username = r.FormValue("username")
			password = r.FormValue("password")
			token, idToken, grantedScope, err = exchanger.LoginLocal(r.Context(), username, password, scope, clientId, AuthMethodOAuth2)
		case "refresh_token":
			// TODO: Implement refresh_token exchange once refresh token storage is added
			err = fmt.Errorf("grant_type refresh_token not yet supported for local exchange")
			http.Error(w, "grant_type refresh_token not yet supported for local exchange", http.StatusNotImplemented)
			return
		default:
			err = fmt.Errorf("Unsupported grant_type")
			http.Error(w, "Unsupported grant_type", http.StatusBadRequest)
			return
		}

		if err != nil {
			err = fmt.Errorf("Authentication failed: %w", err)
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
			err = fmt.Errorf("Failed to encode token response: %w", err)
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
