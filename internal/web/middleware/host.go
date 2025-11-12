package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	store_ "kdex.dev/web/internal/store"
)

type contextKey string

const HostKey contextKey = "host"

func WithHost(store *store_.HostStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hostHeader := r.Host

			if strings.Contains(hostHeader, ":") {
				hostHeader = strings.Split(hostHeader, ":")[0]
			}

			handlers := store.List()

			var bestMatchHost *store_.HostHandler
			var bestMatchLength = -1

			for _, handler := range handlers {
				for _, domain := range handler.Domains() {
					if domain == hostHeader {
						ctx := context.WithValue(r.Context(), HostKey, handler)
						next.ServeHTTP(w, r.WithContext(ctx))
						return
					}

					if strings.HasPrefix(domain, "*.") {
						suffix := domain[1:]
						if strings.HasSuffix(hostHeader, suffix) && len(hostHeader) > len(suffix) {
							if len(domain) > bestMatchLength {
								bestMatchLength = len(domain)
								bestMatchHost = handler
							}
						}
					}
				}
			}

			if bestMatchHost == nil {
				bestMatchHost = &fallbackHostHandler
			}

			if bestMatchHost != nil {
				ctx := context.WithValue(r.Context(), HostKey, bestMatchHost)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

var fallbackHostHandler = store_.HostHandler{
	Mux: func() *http.ServeMux {
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			_, err := fmt.Fprintf(w, "Welcome to KDex!")

			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		})
		return mux
	}(),
}
