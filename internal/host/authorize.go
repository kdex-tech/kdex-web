package host

import (
	"net/http"
	"net/url"
	"slices"

	"github.com/kdex-tech/kdex-host/internal/auth"
)

func (hh *HostHandler) AuthorizeHandler(w http.ResponseWriter, r *http.Request) {
	// 1. Validate parameters
	clientID := r.URL.Query().Get("client_id")
	redirectURI := r.URL.Query().Get("redirect_uri")
	responseType := r.URL.Query().Get("response_type")
	scope := r.URL.Query().Get("scope")
	state := r.URL.Query().Get("state")

	if clientID == "" {
		http.Error(w, "Missing client_id", http.StatusBadRequest)
		return
	}

	if responseType != "code" {
		http.Error(w, "Unsupported response_type", http.StatusBadRequest)
		return
	}

	authClient, ok := hh.authExchanger.GetClient(clientID)
	if !ok {
		http.Error(w, "Invalid client_id", http.StatusBadRequest)
		return
	}

	if !slices.Contains(authClient.RedirectURIs, redirectURI) {
		http.Error(w, "Invalid redirect_uri", http.StatusBadRequest)
		return
	}

	// 2. Parse redirect_uri
	callbackURL, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "Invalid redirect_uri", http.StatusBadRequest)
		return
	}

	// 3. Check Authentication
	ctx := r.Context()
	authCtx, ok := auth.GetAuthContext(ctx)
	if !ok {
		// Not logged in -> Redirect to Login
		// Encode current URL as return URL
		returnURL := r.URL.String()
		http.Redirect(w, r, "/-/login?return="+url.QueryEscape(returnURL), http.StatusSeeOther)
		return
	}

	// We need the Subject.
	subject, err := authCtx.GetSubject()
	if err != nil {
		hh.log.Error(err, "Failed to get subject from auth context")
		http.Error(w, "Invalid session", http.StatusInternalServerError)
		return
	}

	// 4. Generate Authorization Code
	claims := auth.AuthorizationCodeClaims{
		Subject:     subject,
		ClientID:    clientID,
		Scope:       scope,
		RedirectURI: redirectURI,
		AuthMethod:  auth.AuthMethodOAuth2,
	}

	code, err := hh.authExchanger.CreateAuthorizationCode(r.Context(), claims)
	if err != nil {
		hh.log.Error(err, "Failed to create auth code")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	q := callbackURL.Query()
	q.Set("code", code)
	if state != "" {
		q.Set("state", state)
	}
	callbackURL.RawQuery = q.Encode()

	http.Redirect(w, r, callbackURL.String(), http.StatusFound)
}
