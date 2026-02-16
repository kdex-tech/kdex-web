package host

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/kdex-tech/dmapper"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/web/internal/auth"
	"kdex.dev/web/internal/sign"
)

func (hh *HostHandler) reverseProxyHandler(fn *kdexv1alpha1.KDexFunction) http.HandlerFunc {
	target, err := url.Parse(fn.Status.URL)
	if err != nil {
		return func(w http.ResponseWriter, r *http.Request) {
			hh.log.Error(err, "failed to parse function URL", "url", fn.Status.URL)
			http.Error(w, "invalid function URL", http.StatusInternalServerError)
		}
	}

	var mapper *dmapper.Mapper
	if fn.Spec.ClaimMappings != nil {
		mapper, err = dmapper.NewMapper(fn.Spec.ClaimMappings)
		if err != nil {
			return func(w http.ResponseWriter, r *http.Request) {
				hh.log.Error(err, "failed to create mapper", "mapper", fn.Spec.ClaimMappings)
				http.Error(w, "invalid mapper", http.StatusInternalServerError)
			}
		}
	}

	signer, err := sign.NewSigner(
		fn.Status.URL,
		5*time.Minute,
		hh.issuerAddress(),
		&hh.authConfig.ActivePair.Private,
		hh.authConfig.ActivePair.KeyId,
		mapper,
	)

	proxy := &httputil.ReverseProxy{
		Rewrite: func(preq *httputil.ProxyRequest) {
			hh.log.V(2).Info("PROXY: modifying request", "url", preq.In.URL)
			// 1. Set Target and Host
			preq.Out.URL.Scheme = target.Scheme
			preq.Out.URL.Host = target.Host
			preq.Out.Host = target.Host // Essential for FaaS routing

			// 2. Precise Path Joining
			// Note: We do NOT strip the BasePath because KDex functions are
			// implemented using the full paths defined in their OpenAPI spec.
			preq.Out.URL.Path = path.Join(target.Path, preq.In.URL.Path)
			if strings.HasSuffix(preq.In.URL.Path, "/") && !strings.HasSuffix(preq.Out.URL.Path, "/") {
				preq.Out.URL.Path += "/"
			}

			// 3. Forward Query Parameters exactly
			// This copies the encoded query string (e.g., ?user=123&sort=asc)
			preq.Out.URL.RawQuery = preq.In.URL.RawQuery

			signingContext, isLoggedIn := auth.GetClaims(preq.In.Context())
			if isLoggedIn {
				cookies := map[string]any{}
				for _, cookie := range preq.In.Cookies() {
					cookies[cookie.Name] = cookie.Value
				}
				if len(cookies) > 0 {
					signingContext["cookies"] = cookies
				}
				headers := map[string]any{}
				for key, value := range preq.In.Header {
					headers[key] = value
				}
				if len(headers) > 0 {
					signingContext["headers"] = headers
				}

				token, err := signer.Sign(signingContext)
				if err != nil {
					hh.log.Error(err, "failed to sign token")
				} else {
					preq.Out.Header.Set("Authorization", "Bearer "+token)
				}
			} else {
				preq.Out.Header.Del("Authorization")
			}

			preq.Out.Header.Del("Cookie")

			// 4. Standard Proxy Headers
			preq.Out.Header.Set("X-Kdex-Forwarded", "true")
			preq.SetXForwarded()
		},
		ModifyResponse: func(resp *http.Response) error {
			hh.log.V(2).Info("PROXY: modifying response", "url", resp.Request.URL)
			// 5. Rewrite Set-Cookie Domain
			// This ensures cookies from the FaaS backend are tied to your proxy domain
			cookies := resp.Header["Set-Cookie"]
			for i, cookie := range cookies {
				// We remove the specific Domain attribute so the browser
				// defaults to the domain the user actually visited (your proxy).
				// You could also explicitly replace it with your proxy's domain.
				resp.Header["Set-Cookie"][i] = hh.stripCookieDomain(cookie)
			}
			return nil
		},
		// TODO: make transport configurable
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   5 * time.Second, // Connection timeout
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ResponseHeaderTimeout: 15 * time.Second, // Wait for FaaS headers
			IdleConnTimeout:       90 * time.Second,
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			hh.log.Error(err, "PROXY: backend failure", "url", r.URL.String())

			code := http.StatusBadGateway
			if errors.Is(err, context.DeadlineExceeded) {
				code = http.StatusGatewayTimeout
			}

			http.Error(w, err.Error(), code)
		},
	}

	// Capture the start time and log the completion
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		defer func() {
			code := http.StatusOK
			if ew := GetErrorResponseWriter(w); ew != nil {
				code = ew.statusCode
			}

			// Log the Completion
			hh.log.V(2).Info("proxy request finished",
				"function", fn.Name,
				"statusCode", code,
				"duration", time.Since(start).String(),
			)
		}()

		// Log the Inbound Request
		hh.log.V(2).Info("proxy request started",
			"function", fn.Name,
			"method", r.Method,
			"path", r.URL.Path,
			"target", target.String(),
		)

		if shouldReturn := hh.handleAuth(
			r,
			w,
			"functions",
			fn.Spec.API.BasePath,
			functionCallRequirements(r, fn),
		); shouldReturn {
			return
		}

		// Execute the proxy
		proxy.ServeHTTP(w, r)

		r.Header.Set("X-KDex-Sniffer-Skip", "true")
	}
}
