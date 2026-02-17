package server

import (
	"net/http"

	"github.com/kdex-tech/kdex-host/internal/host"
	"github.com/kdex-tech/kdex-host/internal/web/middleware"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func New(address string, hostHandler *host.HostHandler) *http.Server {
	handler := middleware.WithLogger(
		logf.Log.WithName("server"),
	)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hostHandler.ServeHTTP(w, r)
		}),
	)

	return &http.Server{
		Addr:    address,
		Handler: handler,
	}
}
