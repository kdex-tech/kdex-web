package host

import (
	"net/http"

	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	kdexhttp "kdex.dev/web/internal/http"
	"kdex.dev/web/internal/page"
)

func (hh *HostHandler) pageHandlerFunc(
	pageHandler page.PageHandler,
	l10nRenders map[string]string,
) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		shouldReturn := hh.handleAuth(r, w, "pages", pageHandler.BasePath(), hh.pageRequirements(&pageHandler))
		if shouldReturn {
			return
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

		hh.log.V(1).Info("serving", "page", pageHandler.Name, "basePath", pageHandler.BasePath(), "language", l.String())

		w.Header().Set("Content-Language", l.String())
		w.Header().Set("Content-Type", "text/html")

		_, err = w.Write([]byte(rendered))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func (hh *HostHandler) handleAuth(
	r *http.Request,
	w http.ResponseWriter,
	resource string,
	resourceName string,
	requirements []kdexv1alpha1.SecurityRequirement,
) bool {
	if !hh.authConfig.IsAuthEnabled() {
		return false
	}

	authorized, err := hh.authChecker.CheckAccess(
		r.Context(), resource, resourceName, requirements)

	if err != nil {
		hh.log.Error(err, "authorization check failed", resource, resourceName)
		http.Error(w, http.StatusText(http.StatusNotFound)+" "+r.URL.Path, http.StatusNotFound)
		return true
	}

	if !authorized {
		hh.log.V(1).Info("unauthorized access attempt", resource, resourceName)
		r.Header.Set("X-KDex-Sniffer-Skip", "true")
		http.Error(w, http.StatusText(http.StatusNotFound)+" "+r.URL.Path, http.StatusNotFound)
		return true
	}

	return false
}
