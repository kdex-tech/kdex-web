package server

import (
	"net/http"

	store_ "kdex.dev/web/internal/store"
	"kdex.dev/web/internal/web/middleware"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func New(address string, store *store_.HostStore) *http.Server {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hostHandler, ok := r.Context().Value(middleware.HostKey).(*store_.HostHandler)
		if ok {
			hostHandler.ServeHTTP(w, r)
			return
		}

		log := logf.FromContext(r.Context())

		log.Info("no host was found")

		http.NotFound(w, r)
	})

	hostHandler := middleware.WithHost(store)(handler)

	return &http.Server{
		Addr:    address,
		Handler: hostHandler,
	}
}
