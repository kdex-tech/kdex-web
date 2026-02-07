package host

import (
	"net/http"
	"net/url"

	"kdex.dev/web/internal/auth"
	kdexhttp "kdex.dev/web/internal/http"
	"kdex.dev/web/internal/page"
)

func (hh *HostHandler) pageHandlerFunc(
	basePath string,
	name string,
	l10nRenders map[string]string,
	pageHandler page.PageHandler,
) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if hh.authConfig.IsAuthEnabled() {
			// Check authorization before processing the request

			// Perform authorization check
			authorized, err := hh.authChecker.CheckAccess(
				r.Context(), "pages", basePath, hh.pageRequirements(&pageHandler))

			if err != nil {
				hh.log.Error(err, "authorization check failed", "page", name, "basePath", basePath)
				http.Error(w, http.StatusText(http.StatusNotFound)+" "+r.URL.Path, http.StatusNotFound)
				return
			}

			// User is not authorized
			if !authorized {
				hh.log.V(1).Info("unauthorized access attempt", "page", name, "basePath", basePath)

				// But is logged in, error page
				if _, isLoggedIn := auth.GetClaims(r.Context()); isLoggedIn {
					r.Header.Set("X-KDex-Sniffer-Skip", "true")
					http.Error(w, http.StatusText(http.StatusNotFound)+" "+r.URL.Path, http.StatusNotFound)
					return
				}

				// Redirect to login with return URL
				returnURL := r.URL.Path
				if r.URL.RawQuery != "" {
					returnURL += "?" + r.URL.RawQuery
				}
				redirectURL := "/-/login?return=" + url.QueryEscape(returnURL)
				http.Redirect(w, r, redirectURL, http.StatusSeeOther)
				return
			}
		}

		l, err := kdexhttp.GetLang(r, hh.defaultLanguage, hh.Translations.Languages())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		rendered, ok := l10nRenders[l.String()]

		if !ok {
			http.Error(w, http.StatusText(http.StatusNotFound)+" "+r.URL.Path, http.StatusNotFound)
			return
		}

		hh.log.V(1).Info("serving", "page", name, "basePath", basePath, "language", l.String())

		w.Header().Set("Content-Language", l.String())
		w.Header().Set("Content-Type", "text/html")

		_, err = w.Write([]byte(rendered))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
