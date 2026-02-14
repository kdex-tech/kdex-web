package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
	"time"

	openapi "github.com/getkin/kin-openapi/openapi3"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/web/internal/auth"
	ko "kdex.dev/web/internal/openapi"
	"kdex.dev/web/internal/utils"
)

func (hh *HostHandler) addHandlerAndRegister(mux *http.ServeMux, pr pageRender, registeredPaths map[string]ko.PathInfo) {
	finalPath := toFinalPath(pr.ph.BasePath())
	label := pr.ph.Label()

	handler := hh.pageHandlerFunc(finalPath, pr.ph.Name, pr.l10nRenders, pr.ph)

	regFunc := func(p string, n string, l string, pattern bool, localized bool) {
		reqs := hh.convertRequirements(pr.ph.Page.Security)

		op := &openapi.Operation{
			Description: fmt.Sprintf("Get HTML for %s%s%s", l, utils.IfElse(pattern, " (pattern)", ""), utils.IfElse(localized, " (localized)", "")),
			OperationID: fmt.Sprintf("%s%s%s-get", n, utils.IfElse(pattern, "-pattern", ""), utils.IfElse(localized, "-localized", "")),
			Parameters:  ko.ExtractParameters(p, "", http.Header{}),
			Responses: openapi.NewResponses(
				openapi.WithStatus(200, &openapi.ResponseRef{
					Value: &openapi.Response{
						Content: openapi.Content{
							"text/html": &openapi.MediaType{
								Schema: &openapi.SchemaRef{
									Value: &openapi.Schema{
										Format: "html",
										Type:   &openapi.Types{openapi.TypeString},
									},
								},
							},
						},
						Description: openapi.Ptr(fmt.Sprintf("HTML for %s%s%s", l, utils.IfElse(pattern, " (pattern)", ""), utils.IfElse(localized, " (localized)", ""))),
					},
				}),
				openapi.WithStatus(303, &openapi.ResponseRef{
					Ref: "#/components/responses/SeeOther",
				}),
				openapi.WithStatus(400, &openapi.ResponseRef{
					Ref: "#/components/responses/BadRequest",
				}),
				openapi.WithStatus(404, &openapi.ResponseRef{
					Ref: "#/components/responses/NotFound",
				}),
				openapi.WithStatus(500, &openapi.ResponseRef{
					Ref: "#/components/responses/InternalServerError",
				}),
			),
			Security: reqs,
			Summary:  fmt.Sprintf("Get %s%s%s", l, utils.IfElse(pattern, " (pattern)", ""), utils.IfElse(localized, " (localized)", "")),
			Tags:     []string{n, "page"},
		}

		hh.registerPath(p, ko.PathInfo{
			API: ko.OpenAPI{
				BasePath: p,
				Paths: map[string]ko.PathItem{
					p: {
						Description: fmt.Sprintf("HTML page %s%s%s", l, utils.IfElse(pattern, " (pattern)", ""), utils.IfElse(localized, " (localized)", "")),
						Get:         op,
						Summary:     fmt.Sprintf("Page %s%s%s", l, utils.IfElse(pattern, " (pattern)", ""), utils.IfElse(localized, " (localized)", "")),
					},
				},
			},
			Type: ko.PagePathType,
		}, registeredPaths)
	}

	mux.HandleFunc("GET "+finalPath, handler)
	mux.HandleFunc("GET /{l10n}"+finalPath, handler)

	regFunc(finalPath, pr.ph.Name, label, false, false)
	regFunc("/{l10n}"+finalPath, pr.ph.Name, label, false, true)

	if pr.ph.Page.PatternPath != "" {
		mux.HandleFunc("GET "+pr.ph.Page.PatternPath, handler)
		mux.HandleFunc("GET /{l10n}"+pr.ph.Page.PatternPath, handler)

		regFunc(pr.ph.Page.PatternPath, pr.ph.Name, label, true, false)
		regFunc("/{l10n}"+pr.ph.Page.PatternPath, pr.ph.Name, label, true, true)
	}
}

func (hh *HostHandler) discoveryHandler(mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	if !hh.authConfig.IsAuthEnabled() {
		return
	}

	const oauth2path = "/.well-known/oauth-authorization-server"
	mux.HandleFunc("GET "+oauth2path, func(w http.ResponseWriter, r *http.Request) {
		issuer := hh.serverAddress(r)
		auth.DiscoveryHandler(issuer)(w, r)
	})
	registeredPaths[oauth2path] = ko.PathInfo{
		API: ko.OpenAPI{
			BasePath: oauth2path,
			Paths: map[string]ko.PathItem{
				oauth2path: {
					Description: "Serve the OAuth 2.0 Authorization Server configuration",
					Get: &openapi.Operation{
						Description: "GET the OAuth 2.0 Authorization Server configuration",
						OperationID: "oauth2-authorization-server-get",
						Responses: openapi.NewResponses(
							openapi.WithName("200", &openapi.Response{
								Content: openapi.NewContentWithSchema(
									&openapi.Schema{
										Format: "json",
										Type:   &openapi.Types{openapi.TypeObject},
									},
									[]string{"application/json"},
								),
								Description: openapi.Ptr("OpenID Configuration"),
							}),
							openapi.WithStatus(500, &openapi.ResponseRef{
								Ref: "#/components/responses/InternalServerError",
							}),
						),
						Summary: "OAuth 2.0 Authorization Server configuration",
						Tags:    []string{"system", "oidc", "auth"},
					},
					Summary: "The OAuth 2.0 Authorization Server configuration",
				},
			},
		},
		Type: ko.SystemPathType,
	}

	const oidcPath = "/.well-known/openid-configuration"
	mux.HandleFunc("GET "+oidcPath, func(w http.ResponseWriter, r *http.Request) {
		issuer := hh.serverAddress(r)
		auth.DiscoveryHandler(issuer)(w, r)
	})
	registeredPaths[oidcPath] = ko.PathInfo{
		API: ko.OpenAPI{
			BasePath: oidcPath,
			Paths: map[string]ko.PathItem{
				oidcPath: {
					Description: "Serve the OpenID configuration",
					Get: &openapi.Operation{
						Description: "GET the OpenID configuration",
						OperationID: "discovery-get",
						Responses: openapi.NewResponses(
							openapi.WithName("200", &openapi.Response{
								Content: openapi.NewContentWithSchema(
									&openapi.Schema{
										Format: "json",
										Type:   &openapi.Types{openapi.TypeObject},
									},
									[]string{"application/json"},
								),
								Description: openapi.Ptr("OpenID Configuration"),
							}),
							openapi.WithStatus(500, &openapi.ResponseRef{
								Ref: "#/components/responses/InternalServerError",
							}),
						),
						Summary: "OpenID Discovery",
						Tags:    []string{"system", "oidc", "auth"},
					},
					Summary: "The OpenID configuration",
				},
			},
		},
		Type: ko.SystemPathType,
	}
}

func (hh *HostHandler) faviconHandler(mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	const path = "/favicon.ico"
	mux.HandleFunc("GET "+path, hh.favicon.FaviconHandler)
	registeredPaths[path] = ko.PathInfo{
		API: ko.OpenAPI{
			BasePath: path,
			Paths: map[string]ko.PathItem{
				path: {
					Description: "The favicon SVG resource",
					Get: &openapi.Operation{
						Description: "GET the favicon SVG",
						OperationID: "favicon-get",
						Responses: openapi.NewResponses(
							openapi.WithName("200", &openapi.Response{
								Content: openapi.NewContentWithSchema(
									&openapi.Schema{
										Format: "xml",
										Type:   &openapi.Types{openapi.TypeString},
									},
									[]string{"image/svg+xml"},
								),
								Description: openapi.Ptr("SVG Favicon"),
							}),
							openapi.WithStatus(500, &openapi.ResponseRef{
								Ref: "#/components/responses/InternalServerError",
							}),
						),
						Summary: "Favicon SVG",
						Tags:    []string{"system", "favicon"},
					},
					Summary: "Favicon SVG resource",
				},
			},
		},
		Type: ko.SystemPathType,
	}
}

func (hh *HostHandler) jwksHandler(mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	if !hh.authConfig.IsAuthEnabled() {
		return
	}

	const path = "/.well-known/jwks.json"
	mux.HandleFunc("GET "+path, auth.JWKSHandler(hh.authConfig.KeyPairs))
	registeredPaths[path] = ko.PathInfo{
		API: ko.OpenAPI{
			BasePath: path,
			Paths: map[string]ko.PathItem{
				path: {
					Description: "Serve the JWT key set",
					Get: &openapi.Operation{
						Description: "GET the JWT key set",
						OperationID: "jwks-get",
						Responses: openapi.NewResponses(
							openapi.WithName("200", &openapi.Response{
								Content: openapi.NewContentWithSchema(
									&openapi.Schema{
										Format: "json",
										Type:   &openapi.Types{openapi.TypeString},
									},
									[]string{"application/json"},
								),
								Description: openapi.Ptr("JWKS"),
							}),
							openapi.WithStatus(500, &openapi.ResponseRef{
								Ref: "#/components/responses/InternalServerError",
							}),
						),
						Summary: "The JWKS",
						Tags:    []string{"system", "jwks", "jwt", "auth"},
					},
					Summary: "The JWT key set",
				},
			},
		},
		Type: ko.SystemPathType,
	}
}

func (hh *HostHandler) loginHandler(mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	if !hh.authConfig.IsAuthEnabled() {
		return
	}

	const loginPath = "/-/login"
	mux.HandleFunc("GET "+loginPath, hh.LoginGet)
	mux.HandleFunc("POST "+loginPath, hh.LoginPost)

	hh.registerPath(loginPath, ko.PathInfo{
		API: ko.OpenAPI{
			BasePath: loginPath,
			Paths: map[string]ko.PathItem{
				loginPath: {
					Description: "Provides the login experience",
					Get: &openapi.Operation{
						Description: "GET the login view",
						OperationID: "login-get",
						Parameters:  ko.ExtractParameters(loginPath, "return=foo", http.Header{}),
						Responses: openapi.NewResponses(
							openapi.WithName("200", &openapi.Response{
								Content: openapi.NewContentWithSchema(
									&openapi.Schema{
										Format: "html",
										Type:   &openapi.Types{openapi.TypeString},
									},
									[]string{"text/html"},
								),
								Description: openapi.Ptr("HTML login page"),
							}),
							openapi.WithStatus(303, &openapi.ResponseRef{
								Ref: "#/components/responses/SeeOther",
							}),
							openapi.WithStatus(400, &openapi.ResponseRef{
								Ref: "#/components/responses/BadRequest",
							}),
							openapi.WithStatus(404, &openapi.ResponseRef{
								Ref: "#/components/responses/NotFound",
							}),
							openapi.WithStatus(500, &openapi.ResponseRef{
								Ref: "#/components/responses/InternalServerError",
							}),
						),
						Summary: "Get login experience",
						Tags:    []string{"system", "login", "auth"},
					},
					Post: &openapi.Operation{
						Description: "POST to login action",
						OperationID: "login-post",
						Responses: openapi.NewResponses(
							openapi.WithStatus(303, &openapi.ResponseRef{
								Ref: "#/components/responses/SeeOther",
							}),
							openapi.WithStatus(400, &openapi.ResponseRef{
								Ref: "#/components/responses/BadRequest",
							}),
						),
						Summary: "Login action",
						Tags:    []string{"system", "login", "auth"},
					},
					Summary: "Login experience",
				},
			},
		},
		Type: ko.SystemPathType,
	}, registeredPaths)

	const logoutPath = "/-/logout"
	mux.HandleFunc("POST "+logoutPath, hh.LogoutPost)

	hh.registerPath(logoutPath, ko.PathInfo{
		API: ko.OpenAPI{
			BasePath: logoutPath,
			Paths: map[string]ko.PathItem{
				logoutPath: {
					Description: "Provides the logout experience",
					Post: &openapi.Operation{
						Description: "POST to logout action",
						OperationID: "logout-post",
						Responses: openapi.NewResponses(
							openapi.WithStatus(302, &openapi.ResponseRef{
								Ref: "#/components/responses/Found",
							}),
							openapi.WithStatus(500, &openapi.ResponseRef{
								Ref: "#/components/responses/InternalServerError",
							}),
						),
						Summary: "Logout action",
						Tags:    []string{"system", "logout", "auth"},
					},
					Summary: "Logout experience",
				},
			},
		},
		Type: ko.SystemPathType,
	}, registeredPaths)
}

func (hh *HostHandler) navigationHandler(mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	const path = "/-/navigation/{navKey}/{l10n}/{basePathMinusLeadingSlash...}"
	mux.HandleFunc("GET "+path, hh.NavigationGet)

	hh.registerPath(path, ko.PathInfo{
		API: ko.OpenAPI{
			BasePath: path,
			Paths: map[string]ko.PathItem{
				path: {
					Description: "Dynamic HTML navigation components, supporting localization and breadcrumb contexts.",
					Get: &openapi.Operation{
						Description: "GET Dynamic HTML navigation",
						OperationID: "navigation-get",
						Parameters:  ko.ExtractParameters(path, "", http.Header{}),
						Responses: openapi.NewResponses(
							openapi.WithName("200", &openapi.Response{
								Content: openapi.NewContentWithSchema(
									&openapi.Schema{
										Format: "html",
										Type:   &openapi.Types{openapi.TypeString},
									},
									[]string{"text/html"},
								),
								Description: openapi.Ptr("HTML navigation fragment"),
							}),
							openapi.WithStatus(400, &openapi.ResponseRef{
								Ref: "#/components/responses/BadRequest",
							}),
							openapi.WithStatus(404, &openapi.ResponseRef{
								Ref: "#/components/responses/NotFound",
							}),
							openapi.WithStatus(500, &openapi.ResponseRef{
								Ref: "#/components/responses/InternalServerError",
							}),
						),
						Summary: "Dynamic HTML navigation",
						Tags:    []string{"system", "navigation"},
					},
					Summary: "Dynamic HTML navigation components",
				},
			},
		},
		Type: ko.SystemPathType,
	}, registeredPaths)
}

func (hh *HostHandler) oauthHandler(mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	if !hh.authConfig.IsOIDCEnabled() {
		return
	}

	const path = "/-/oauth/callback"
	mux.HandleFunc("GET "+path, hh.OAuthGet)

	hh.registerPath(path, ko.PathInfo{
		API: ko.OpenAPI{
			BasePath: path,
			Paths: map[string]ko.PathItem{
				path: {
					Description: "The OAuth2 support endpoint",
					Get: &openapi.Operation{
						Description: "GET OAuth2 Callback",
						OperationID: "oauth-get",
						Parameters:  ko.ExtractParameters(path, "code=foo&state=bar", http.Header{}),
						Responses: openapi.NewResponses(
							openapi.WithStatus(303, &openapi.ResponseRef{
								Ref: "#/components/responses/SeeOther",
							}),
							openapi.WithStatus(400, &openapi.ResponseRef{
								Ref: "#/components/responses/BadRequest",
							}),
							openapi.WithStatus(401, &openapi.ResponseRef{
								Ref: "#/components/responses/Unauthorized",
							}),
							openapi.WithStatus(500, &openapi.ResponseRef{
								Ref: "#/components/responses/InternalServerError",
							}),
						),
						Summary: "OAuth2 Callback",
						Tags:    []string{"system", "oauth2", "auth"},
					},
					Summary: "OAuth2 support",
				},
			},
		},
		Type: ko.SystemPathType,
	}, registeredPaths)
}

func (hh *HostHandler) openapiHandler(mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	const path = "/-/openapi"

	mux.HandleFunc("GET "+path, hh.OpenAPIGet)

	// Register the path itself so it appears in the spec
	hh.registerPath(path, ko.PathInfo{
		API: ko.OpenAPI{
			BasePath: path,
			Paths: map[string]ko.PathItem{
				path: {
					Description: "Serves the generated OpenAPI 3.0 specification for this host.",
					Get: &openapi.Operation{
						Description: "GET OpenAPI 3.0 Spec",
						OperationID: "openapi-get",
						Parameters: ko.ExtractParameters(
							path, "path=one&path=two&tag=one&tag=two&type=one&type=two",
							http.Header{},
						),
						Responses: openapi.NewResponses(
							openapi.WithName("200", &openapi.Response{
								Content: openapi.NewContentWithSchema(
									&openapi.Schema{
										AdditionalProperties: openapi.AdditionalProperties{
											Has: openapi.Ptr(true),
										},
										Type: &openapi.Types{openapi.TypeObject},
									},
									[]string{"application/json"},
								),
								Description: openapi.Ptr("OpenAPI documentation"),
							}),
							openapi.WithStatus(500, &openapi.ResponseRef{
								Ref: "#/components/responses/InternalServerError",
							}),
						),
						Summary: "OpenAPI 3.0 Spec",
						Tags:    []string{"system", "openapi"},
					},
					Summary: "Generated OpenAPI 3.0 specification",
				},
			},
		},
		Type: ko.SystemPathType,
	}, registeredPaths)
}

func (hh *HostHandler) reverseProxyHandler(fn *kdexv1alpha1.KDexFunction) http.HandlerFunc {
	target, err := url.Parse(fn.Status.URL)
	if err != nil {
		return func(w http.ResponseWriter, r *http.Request) {
			hh.log.Error(err, "failed to parse function URL", "url", fn.Status.URL)
			http.Error(w, "invalid function URL", http.StatusInternalServerError)
		}
	}

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

		// Log the Inbound Request
		hh.log.V(2).Info("proxy request started",
			"function", fn.Name,
			"method", r.Method,
			"path", r.URL.Path,
			"target", target.String(),
		)

		// Execute the proxy
		proxy.ServeHTTP(w, r)

		r.Header.Set("X-KDex-Sniffer-Skip", "true")

		code := http.StatusOK
		if ew := GetErrorResponseWriter(w); ew != nil {
			code = ew.statusCode
		}

		// Log the Completion
		// Note: If you want the status code here, use the 'ew'
		// reference from your errorResponseWriter middleware!
		hh.log.V(2).Info("proxy request finished",
			"function", fn.Name,
			"statusCode", code,
			"duration", time.Since(start).String(),
		)
	}
}

func (hh *HostHandler) schemaHandler(mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	const path = "/-/schema/{path...}"

	mux.HandleFunc("GET "+path, hh.SchemaGet)

	// Register the path itself so it appears in the spec
	hh.registerPath(path, ko.PathInfo{
		API: ko.OpenAPI{
			BasePath: path,
			Paths: map[string]ko.PathItem{
				path: {
					Description: "Serves individual JSONschema from the registered OpenAPI specifications. The path should be in the format /-/schema/{basePath}/{schemaName} (e.g., /-/schema/v1/users/User) or simply /-/schema/{schemaName} for a global lookup.",
					Get: &openapi.Operation{
						Description: "GET JSONschema",
						OperationID: "schema-get",
						Parameters:  ko.ExtractParameters(path, "", http.Header{}),
						Responses: openapi.NewResponses(
							openapi.WithName("200", &openapi.Response{
								Content: openapi.NewContentWithSchema(
									&openapi.Schema{
										Type: &openapi.Types{openapi.TypeObject},
									},
									[]string{"application/json"},
								),
								Description: openapi.Ptr("JSONschema fragment"),
							}),
							openapi.WithStatus(404, &openapi.ResponseRef{
								Ref: "#/components/responses/NotFound",
							}),
							openapi.WithStatus(500, &openapi.ResponseRef{
								Ref: "#/components/responses/InternalServerError",
							}),
						),
						Summary: "JSONschema",
						Tags:    []string{"system", "jsonschema", "schema", "openapi"},
					},
					Summary: "JSONschema Provider",
				},
			},
		},
		Type: ko.SystemPathType,
	}, registeredPaths)
}

func (hh *HostHandler) snifferHandler(mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	if hh.sniffer != nil {
		const inspectPath = "/-/sniffer/inspect/{uuid}"
		mux.HandleFunc("GET "+inspectPath, hh.InspectHandler)

		hh.registerPath(inspectPath, ko.PathInfo{
			API: ko.OpenAPI{
				BasePath: inspectPath,
				Paths: map[string]ko.PathItem{
					inspectPath: {
						Description: "Provides inspection dashboard for the Request Sniffer's computed results.",
						Get: &openapi.Operation{
							Description: "GET Sniffer dashboard",
							OperationID: "sniffer-dashboard-get",
							Parameters:  ko.ExtractParameters(inspectPath, "format=text", http.Header{}),
							Responses: openapi.NewResponses(
								openapi.WithName("200", &openapi.Response{
									Description: openapi.Ptr("Dashboard"),
									Content: openapi.NewContentWithSchema(
										&openapi.Schema{
											Format: "text",
											Type:   &openapi.Types{openapi.TypeString},
										},
										[]string{"text/plain"},
									),
								}),
								openapi.WithName("200", &openapi.Response{
									Description: openapi.Ptr("Dashboard"),
									Content: openapi.NewContentWithSchema(
										&openapi.Schema{
											Format: "html",
											Type:   &openapi.Types{openapi.TypeString},
										},
										[]string{"text/html"},
									),
								}),
								openapi.WithStatus(404, &openapi.ResponseRef{
									Ref: "#/components/responses/NotFound",
								}),
								openapi.WithStatus(500, &openapi.ResponseRef{
									Ref: "#/components/responses/InternalServerError",
								}),
							),
							Summary: "Sniffer Dashboard",
							Tags:    []string{"system", "sniffer", "dashboard"},
						},
						Summary: "Provides inspection dashboard",
					},
				},
			},
			Type: ko.SystemPathType,
		}, registeredPaths)

		const docsPath = "/-/sniffer/docs"
		mux.HandleFunc("GET "+docsPath, hh.sniffer.DocsHandler)

		hh.registerPath(docsPath, ko.PathInfo{
			API: ko.OpenAPI{
				BasePath: docsPath,
				Paths: map[string]ko.PathItem{
					docsPath: {
						Description: "Provides Markdown documentation for the Request Sniffer's supported headers and behaviors.",
						Get: &openapi.Operation{
							Description: "GET Sniffer Docs",
							OperationID: "sniffer-docs-get",
							Parameters:  ko.ExtractParameters(docsPath, "", http.Header{}),
							Responses: openapi.NewResponses(
								openapi.WithName("200", &openapi.Response{
									Description: openapi.Ptr("Markdown"),
									Content: openapi.NewContentWithSchema(
										&openapi.Schema{
											Format: "markdown",
											Type:   &openapi.Types{openapi.TypeString},
										},
										[]string{"text/markdown"},
									),
								}),
								openapi.WithStatus(500, &openapi.ResponseRef{
									Ref: "#/components/responses/InternalServerError",
								}),
							),
							Summary: "Sniffer Docs",
							Tags:    []string{"system", "sniffer", "docs"},
						},
						Summary: "Request Sniffer Documentation",
					},
				},
			},
			Type: ko.SystemPathType,
		}, registeredPaths)
	}
}

func (hh *HostHandler) stateHandler(mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	const path = "/-/state/"
	mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
		claims, ok := auth.GetClaims(r.Context())
		if !ok {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(claims); err != nil {
			hh.log.Error(err, "failed to encode claims")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
	})

	hh.registerPath(path, ko.PathInfo{
		API: ko.OpenAPI{
			BasePath: path,
			Paths: map[string]ko.PathItem{
				path: {
					Description: "Returns the current authenticated session state (claims) without requiring the client to parse the JWT.",
					Get: &openapi.Operation{
						Description: "GET authenticated session state",
						OperationID: "state-get",
						Responses: openapi.NewResponses(
							openapi.WithName("200", &openapi.Response{
								Content: openapi.NewContentWithSchema(
									&openapi.Schema{
										Format: "json",
										Type:   &openapi.Types{openapi.TypeObject},
									},
									[]string{"application/json"},
								),
								Description: openapi.Ptr("Current session claims"),
							}),
							openapi.WithStatus(401, &openapi.ResponseRef{
								Ref: "#/components/responses/Unauthorized",
							}),
							openapi.WithStatus(500, &openapi.ResponseRef{
								Ref: "#/components/responses/InternalServerError",
							}),
						),
						Summary: "Authenticated session state",
						Tags:    []string{"system", "state", "auth"},
					},
					Summary: "The current authenticated session state (claims)",
				},
			},
		},
		Type: ko.SystemPathType,
	}, registeredPaths)
}

func (hh *HostHandler) tokenHandler(mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	if !hh.authConfig.IsAuthEnabled() {
		return
	}

	const path = "/-/token"
	mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
		issuer := hh.serverAddress(r)
		auth.OAuth2TokenHandler(hh.authExchanger, issuer)(w, r)
	})
	hh.registerPath(path, ko.PathInfo{
		API: ko.OpenAPI{
			BasePath: path,
			Paths: map[string]ko.PathItem{
				path: {
					Description: "The OAuth2 token endpoint",
					Post: &openapi.Operation{
						Description: "POST to exchange credentials for a token",
						OperationID: "token-post",
						RequestBody: &openapi.RequestBodyRef{
							Value: &openapi.RequestBody{
								Content: openapi.Content{
									"application/x-www-form-urlencoded": &openapi.MediaType{
										Schema: &openapi.SchemaRef{
											Value: &openapi.Schema{
												Properties: openapi.Schemas{
													"client_id": &openapi.SchemaRef{
														Value: &openapi.Schema{
															Type: &openapi.Types{openapi.TypeString},
														},
													},
													"grant_type": &openapi.SchemaRef{
														Value: &openapi.Schema{
															Type: &openapi.Types{openapi.TypeString},
														},
													},
													"password": &openapi.SchemaRef{
														Value: &openapi.Schema{
															Type: &openapi.Types{openapi.TypeString},
														},
													},
													"scope": &openapi.SchemaRef{
														Value: &openapi.Schema{
															Type: &openapi.Types{openapi.TypeString},
														},
													},
													"username": &openapi.SchemaRef{
														Value: &openapi.Schema{
															Type: &openapi.Types{openapi.TypeString},
														},
													},
												},
												Required: []string{"grant_type", "client_id"},
												Type:     &openapi.Types{openapi.TypeObject},
											},
										},
									},
								},
								Description: "Token request body",
							},
						},
						Responses: openapi.NewResponses(
							openapi.WithName("200", &openapi.Response{
								Content: openapi.NewContentWithSchema(
									&openapi.Schema{
										Format: "json",
										Type:   &openapi.Types{openapi.TypeObject},
									},
									[]string{"application/json"},
								),
								Description: openapi.Ptr("Token Response"),
							}),
							openapi.WithStatus(400, &openapi.ResponseRef{
								Ref: "#/components/responses/BadRequest",
							}),
							openapi.WithStatus(401, &openapi.ResponseRef{
								Ref: "#/components/responses/Unauthorized",
							}),
							openapi.WithStatus(500, &openapi.ResponseRef{
								Ref: "#/components/responses/InternalServerError",
							}),
						),
						Summary: "OAuth2 Token",
						Tags:    []string{"system", "oauth2", "auth"},
					},
					Summary: "The OAuth2 token endpoint",
				},
			},
		},
		Type: ko.SystemPathType,
	}, registeredPaths)
}

func (hh *HostHandler) translationHandler(mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	const path = "/-/translation/{l10n}"
	mux.HandleFunc("GET "+path, hh.TranslationGet)

	hh.registerPath(path, ko.PathInfo{
		API: ko.OpenAPI{
			BasePath: path,
			Paths: map[string]ko.PathItem{
				path: {
					Description: "Provides localization keys and their translated values for a given language tag as JSON.",
					Get: &openapi.Operation{
						Description: "GET localization keys and their translated values",
						OperationID: "translation-get",
						Parameters:  ko.ExtractParameters(path, "key=one&key=two", http.Header{}),
						Responses: openapi.NewResponses(
							openapi.WithName("200", &openapi.Response{
								Description: openapi.Ptr("JSON translation map"),
								Content: openapi.NewContentWithSchema(
									&openapi.Schema{
										AdditionalProperties: openapi.AdditionalProperties{
											Has: openapi.Ptr(true),
										},
										Type: &openapi.Types{openapi.TypeObject},
									},
									[]string{"application/json"},
								),
							}),
							openapi.WithStatus(400, &openapi.ResponseRef{
								Ref: "#/components/responses/BadRequest",
							}),
							openapi.WithStatus(500, &openapi.ResponseRef{
								Ref: "#/components/responses/InternalServerError",
							}),
						),
						Summary: "Localization keys and their translated values",
						Tags:    []string{"system", "translation", "localization"},
					},
					Summary: "Localization keys and their translated values",
				},
			},
		},
		Type: ko.SystemPathType,
	}, registeredPaths)
}
