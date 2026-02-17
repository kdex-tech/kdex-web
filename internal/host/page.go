package host

import (
	"net/http"

	kdexhttp "github.com/kdex-tech/kdex-host/internal/http"
	"github.com/kdex-tech/kdex-host/internal/page"
)

func (hh *HostHandler) pageHandlerFunc(
	pageHandler page.PageHandler,
	l10nRenders map[string]string,
) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		shouldReturn := hh.handleAuth(
			r,
			w,
			"pages",
			pageHandler.BasePath(),
			hh.pageRequirements(&pageHandler),
		)
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
