package host

import (
	"net/http"
	"strings"
)

func (hh *HostHandler) OAuthGet(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" {
		http.Error(w, "No code in request", http.StatusBadRequest)
		return
	}

	// Exchange code for ID Token
	rawIDToken, err := hh.authExchanger.ExchangeCode(r.Context(), code)
	if err != nil {
		hh.log.Error(err, "failed to exchange oauth code")
		http.Error(w, "Failed to exchange token", http.StatusUnauthorized)
		return
	}

	// Exchange ID Token for Local Token
	localToken, err := hh.authExchanger.ExchangeToken(r.Context(), rawIDToken)
	if err != nil {
		hh.log.Error(err, "failed to exchange for local token")
		http.Error(w, "Failed to exchange for local token", http.StatusUnauthorized)
		return
	}

	store := hh.authConfig.OIDC.IDTokenStore

	if err := store.Set(w, r, rawIDToken); err != nil {
		hh.log.Error(err, "failed to store session hint")
		http.Error(w, "Failed to store session hint", http.StatusInternalServerError)
		return
	}

	// Set Cookie
	http.SetCookie(w, &http.Cookie{
		Name:     hh.authConfig.CookieName,
		Value:    localToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   hh.isSecure(),
		SameSite: http.SameSiteLaxMode,
	})

	// Validate state/redirect
	redirectURL := state
	if redirectURL == "" || !strings.HasPrefix(redirectURL, "/") {
		redirectURL = "/"
	}

	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}
