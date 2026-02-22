package host

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"

	"github.com/kdex-tech/kdex-host/internal/auth"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func (hh *HostHandler) AuthorizeHandler(w http.ResponseWriter, r *http.Request) {
	var clientId, code, redirectURI, responseType, scope, subject, state string
	var callbackURL *url.URL
	var err error

	log := logf.FromContext(r.Context())
	defer func() {
		callbackURLStr := ""
		if callbackURL != nil {
			callbackURLStr = callbackURL.String()
		}
		log.Info(
			"OAuth2 authorization",
			"callback_url", callbackURLStr,
			"client_id", clientId,
			"code", code,
			"redirect_uri", redirectURI,
			"response_type", responseType,
			"scope", scope,
			"subject", subject,
			"state", state,
			"error", err)
	}()

	// 1. Validate parameters
	clientId = r.URL.Query().Get("client_id")
	redirectURI = r.URL.Query().Get("redirect_uri")
	responseType = r.URL.Query().Get("response_type")
	scope = r.URL.Query().Get("scope")
	state = r.URL.Query().Get("state")

	if clientId == "" {
		err = fmt.Errorf("Missing client_id")
		http.Error(w, "Missing client_id", http.StatusBadRequest)
		return
	}

	if responseType != "code" {
		err = fmt.Errorf("Unsupported response_type")
		http.Error(w, "Unsupported response_type", http.StatusBadRequest)
		return
	}

	authClient, ok := hh.authExchanger.GetClient(clientId)
	if !ok {
		err = fmt.Errorf("Invalid client_id")
		http.Error(w, "Invalid client_id", http.StatusBadRequest)
		return
	}

	if !slices.Contains(authClient.RedirectURIs, redirectURI) {
		err = fmt.Errorf("Invalid redirect_uri")
		http.Error(w, "Invalid redirect_uri", http.StatusBadRequest)
		return
	}

	// 2. Parse redirect_uri
	callbackURL, err = url.Parse(redirectURI)
	if err != nil {
		err = fmt.Errorf("Invalid redirect_uri")
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
	subject, err = authCtx.GetSubject()
	if err != nil {
		err = fmt.Errorf("Failed to get subject from auth context")
		http.Error(w, "Invalid session", http.StatusInternalServerError)
		return
	}

	// 4. Generate Authorization Code
	claims := auth.AuthorizationCodeClaims{
		Subject:     subject,
		ClientID:    clientId,
		Scope:       scope,
		RedirectURI: redirectURI,
		AuthMethod:  auth.AuthMethodOAuth2,
	}

	code, err = hh.authExchanger.CreateAuthorizationCode(r.Context(), claims)
	if err != nil {
		err = fmt.Errorf("Failed to create auth code")
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
