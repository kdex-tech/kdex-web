package server

import (
	"net/http"

	"kdex.dev/web/internal/host"
	"kdex.dev/web/internal/web/middleware"
	"kdex.dev/web/pkg/auth"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func New(address string, hostHandler *host.HostHandler, keyPair *auth.KeyPair) *http.Server {
	// TODO: add policy based access control

	// Create a mux to handle both JWKS and application routes
	mux := http.NewServeMux()

	// Register JWKS endpoint
	mux.HandleFunc("/.well-known/jwks.json", auth.JWKSHandler(keyPair.Public))

	// Register main application handler with auth middleware
	mux.Handle("/", middleware.WithLogger(
		logf.Log.WithName("server"),
	)(
		auth.WithAuthentication(keyPair.Public)(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hostHandler.ServeHTTP(w, r)
			}),
		),
	))

	return &http.Server{
		Addr:    address,
		Handler: mux,
	}
}
