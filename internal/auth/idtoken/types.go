package idtoken

import "net/http"

const hintName = "oidc_hint"

type IDTokenStore interface {
	Set(w http.ResponseWriter, r *http.Request, rawIDToken string) error
	Get(r *http.Request) (string, error)
}
