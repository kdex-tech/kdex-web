package server

import (
	"net/http"

	store_ "kdex.dev/web/internal/store"
	"kdex.dev/web/internal/web/middleware"
)

func New(address string, store *store_.HostStore) *http.Server {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		trackedHost, ok := r.Context().Value(middleware.HostKey).(*store_.HostHandler)
		if !ok {
			http.NotFound(w, r)
			return
		}

		trackedHost.ServeHTTP(w, r)
	})

	hostHandler := middleware.WithHost(store)(handler)

	return &http.Server{
		Addr:    address,
		Handler: hostHandler,
	}
}
