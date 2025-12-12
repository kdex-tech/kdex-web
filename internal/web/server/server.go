package server

import (
	"net/http"

	"kdex.dev/web/internal/host"
	"kdex.dev/web/internal/web/middleware"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func New(address string, store *host.HostStore) *http.Server {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hostHandler, ok := r.Context().Value(middleware.HostKey).(*host.HostHandler)
		if ok {
			hostHandler.ServeHTTP(w, r)
			return
		}

		log := logf.FromContext(r.Context())

		log.V(1).Info("no host was found")

		http.NotFound(w, r)
	})

	hostHandler := middleware.WithHost(store)(
		middleware.WithLogger(logf.Log.WithName("server"))(handler),
	)

	return &http.Server{
		Addr:    address,
		Handler: hostHandler,
	}
}
