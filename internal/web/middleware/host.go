package middleware

import (
	"context"
	"net/http"
	"strings"

	"kdex.dev/web/internal/store"
)

type contextKey string

const HostKey contextKey = "host"

func WithHost(store *store.HostStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hostHeader := r.Host

			if strings.Contains(hostHeader, ":") {
				hostHeader = strings.Split(hostHeader, ":")[0]
			}

			hosts := store.List()

			for _, trackedHost := range hosts {
				for _, domain := range trackedHost.Host.Spec.Domains {
					if domain == hostHeader {
						ctx := context.WithValue(r.Context(), HostKey, trackedHost)
						next.ServeHTTP(w, r.WithContext(ctx))
						return
					}
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}
