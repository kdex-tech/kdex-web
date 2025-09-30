package server

import (
	"fmt"
	"net/http"

	"kdex.dev/web/internal/store"
	"kdex.dev/web/internal/web/middleware"
)

func New(port string, store *store.HostStore) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Hello, World!")
	})

	handler := middleware.WithHost(store)(mux)

	return &http.Server{
		Addr:    fmt.Sprintf(":%s", port),
		Handler: handler,
	}
}