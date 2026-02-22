package host

import (
	"fmt"
	"net/http"
	"time"

	kdexhttp "github.com/kdex-tech/kdex-host/internal/http"
	"github.com/kdex-tech/kdex-host/internal/page"
)

func (hh *HostHandler) pageHandlerFunc(
	ph page.PageHandler,
	translations *Translations,
) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		shouldReturn := hh.handleAuth(
			r,
			w,
			"pages",
			ph.BasePath(),
			hh.pageRequirements(&ph),
		)
		if shouldReturn {
			return
		}

		if hh.applyCachingHeaders(w, r, hh.pageRequirements(&ph), time.Time{}) {
			return
		}

		l, err := kdexhttp.GetLang(r, hh.defaultLanguage, translations.Languages())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		cacheKey := fmt.Sprintf("render:%s:%s", ph.Name, l.String())
		rendered, ok, err := hh.renderCache.Get(r.Context(), cacheKey)
		if err != nil {
			hh.log.Error(err, "failed to get from cache", "page", ph.Name, "language", l)
		}

		if !ok {
			rendered, err = hh.L10nRender(ph, nil, l, map[string]any{}, translations)
			if err != nil {
				hh.log.Error(err, "failed to render page", "page", ph.Name, "language", l)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if err := hh.renderCache.Set(r.Context(), cacheKey, rendered); err != nil {
				hh.log.Error(err, "failed to set cache", "page", ph.Name, "language", l)
			}
		}

		hh.log.V(1).Info("serving", "page", ph.Name, "basePath", ph.BasePath(), "language", l.String())

		w.Header().Set("Content-Language", l.String())
		w.Header().Set("Content-Type", "text/html")

		_, err = w.Write([]byte(rendered))
		if err != nil {
			hh.log.Error(err, "failed to write response", "page", ph.Name, "language", l)
		}
	}
}
