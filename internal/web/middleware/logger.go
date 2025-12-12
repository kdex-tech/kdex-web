package middleware

import (
	"net/http"

	"github.com/go-logr/logr"
)

func WithLogger(log logr.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(logr.NewContext(r.Context(), log)))
		})
	}
}
