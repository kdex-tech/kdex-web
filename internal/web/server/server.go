package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/web/internal/menu"
	"kdex.dev/web/internal/render"
	store_ "kdex.dev/web/internal/store"
	"kdex.dev/web/internal/web/middleware"
)

func New(port string, store *store_.HostStore) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		trackedHost, ok := r.Context().Value(middleware.HostKey).(*store_.TrackedHost)
		if !ok {
			http.NotFound(w, r)
			return
		}

		var matchedPage *kdexv1alpha1.MicroFrontEndRenderPage
		var matchedPathLen int

		pages := trackedHost.RenderPages.List()
		for i := range pages {
			page := &pages[i]
			if strings.HasPrefix(r.URL.Path, page.Spec.Path) {
				if len(page.Spec.Path) > matchedPathLen {
					matchedPage = page
					matchedPathLen = len(page.Spec.Path)
				}
			}
		}

		if matchedPage == nil {
			http.NotFound(w, r)
			return
		}

		renderer := render.Renderer{
			Context:      r.Context(),
			Date:         time.Now(),
			FootScript:   "",
			HeadScript:   "",
			Lang:         "en",
			MenuEntries:  menu.ToMenuEntries(trackedHost.RenderPages.List()),
			Meta:         *trackedHost.Host.Spec.BaseMeta,
			Organization: trackedHost.Host.Spec.Organization,
			Request:      r,
			Stylesheet:   trackedHost.Host.Spec.Stylesheet,
		}

		actual, err := renderer.RenderPage(render.Page{
			Contents:        matchedPage.Spec.PageComponents.Contents,
			Footer:          matchedPage.Spec.PageComponents.Footer,
			Header:          matchedPage.Spec.PageComponents.Header,
			Label:           matchedPage.Spec.PageComponents.Title,
			Navigations:     matchedPage.Spec.PageComponents.Navigations,
			TemplateContent: matchedPage.Spec.PageComponents.PrimaryTemplate,
			TemplateName:    matchedPage.Name,
		})

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html")

		_, err = w.Write([]byte(actual))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	handler := middleware.WithHost(store)(mux)

	return &http.Server{
		Addr:    fmt.Sprintf(":%s", port),
		Handler: handler,
	}
}
