package middleware

import (
	"context"
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

			hosts := store.List()

			var bestMatchHost *store_.TrackedHost
			var bestMatchLength = -1

			for _, host := range hosts {
				for _, domain := range host.Host.Spec.Domains {
					if domain == hostHeader {
						ctx := context.WithValue(r.Context(), HostKey, host)
						next.ServeHTTP(w, r.WithContext(ctx))
						return
					}

					if strings.HasPrefix(domain, "*.") {
						suffix := domain[1:]
						if strings.HasSuffix(hostHeader, suffix) && len(hostHeader) > len(suffix) {
							if len(domain) > bestMatchLength {
								bestMatchLength = len(domain)
								bestMatchHost = host
							}
						}
					}
				}
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
