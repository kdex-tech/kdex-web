package server

import (
	"fmt"
	"net/http"
	"strings"

	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	store_ "kdex.dev/web/internal/store"
	"kdex.dev/web/internal/web/middleware"
)

func New(port string, store *store_.HostStore) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
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

		if matchedPage != nil {
			fmt.Fprintf(w, "Hello, %s!", matchedPage.Spec.PageComponents.Title)
			return
		}

		http.NotFound(w, r)
	})

	handler := middleware.WithHost(store)(mux)

	return &http.Server{
		Addr:    fmt.Sprintf(":%s", port),
		Handler: handler,
	}
}
