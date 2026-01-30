package host

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	openapi "github.com/getkin/kin-openapi/openapi3"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/crds/render"
	"kdex.dev/web/internal/auth"
	kdexhttp "kdex.dev/web/internal/http"
	ko "kdex.dev/web/internal/openapi"
	"kdex.dev/web/internal/page"
)

func (hh *HostHandler) addHandlerAndRegister(mux *http.ServeMux, pr pageRender, registeredPaths map[string]ko.PathInfo) {
	finalPath := toFinalPath(pr.ph.BasePath())
	label := pr.ph.Label()

	handler := hh.pageHandlerFunc(finalPath, pr.ph.Name, pr.l10nRenders, pr.ph)

	mux.HandleFunc("GET "+finalPath, handler)
	mux.HandleFunc("GET /{l10n}"+finalPath, handler)

	response := openapi.NewResponses(
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
			},
		}),
	)

	hh.registerPath(finalPath, ko.PathInfo{
		API: ko.OpenAPI{
			BasePath: finalPath,
			Paths: map[string]ko.PathItem{
				finalPath: {
					Description: fmt.Sprintf("Rendered HTML page for %s", label),
					Get: &openapi.Operation{
						Parameters: ko.ExtractParameters(finalPath, "", http.Header{}),
						Responses:  response,
					},
					Summary: label,
				},
			},
		},
		Type: ko.PagePathType,
	}, registeredPaths)

	l10nPath := "/{l10n}" + finalPath
	hh.registerPath(l10nPath, ko.PathInfo{
		API: ko.OpenAPI{
			BasePath: l10nPath,
			Paths: map[string]ko.PathItem{
				l10nPath: {
					Description: fmt.Sprintf("Localized rendered HTML page for %s", label),
					Get: &openapi.Operation{
						Parameters: ko.ExtractParameters(l10nPath, "", http.Header{}),
						Responses:  response,
					},
					Summary: fmt.Sprintf("%s (Localized)", label),
				},
			},
		},
		Type: ko.PagePathType,
	}, registeredPaths)

	patternPath := pr.ph.Page.PatternPath
	l10nPatternPath := "/{l10n}" + patternPath

	if patternPath != "" {
		mux.HandleFunc("GET "+patternPath, handler)
		mux.HandleFunc("GET "+l10nPatternPath, handler)

		hh.registerPath(patternPath, ko.PathInfo{
			API: ko.OpenAPI{
				BasePath: patternPath,
				Paths: map[string]ko.PathItem{
					patternPath: {
						Description: fmt.Sprintf("Rendered HTML page for %s using pattern %s", label, patternPath),
						Get: &openapi.Operation{
							Parameters: ko.ExtractParameters(patternPath, "", http.Header{}),
							Responses:  response,
						},
						Summary: label,
					},
				},
			},
			Type: ko.PagePathType,
		}, registeredPaths)

		hh.registerPath(l10nPatternPath, ko.PathInfo{
			API: ko.OpenAPI{
				BasePath: l10nPatternPath,
				Paths: map[string]ko.PathItem{
					l10nPatternPath: {
						Description: fmt.Sprintf("Localized rendered HTML page for %s using pattern %s", label, l10nPatternPath),
						Get: &openapi.Operation{
							Parameters: ko.ExtractParameters(l10nPatternPath, "", http.Header{}),
							Responses:  response,
						},
						Summary: fmt.Sprintf("%s (Localized)", label),
					},
				},
			},
			Type: ko.PagePathType,
		}, registeredPaths)
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
					Description: "A default favicon",
					Get: &openapi.Operation{
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
						),
					},
					Summary: "The site favicon",
				},
			},
		},
		Type: ko.SystemPathType,
	}
}

func (hh *HostHandler) jwksHandler(mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	if hh.authConfig == nil || hh.authConfig.KeyPairs == nil {
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
						Responses: openapi.NewResponses(
							openapi.WithName("200", &openapi.Response{
								Content: openapi.NewContentWithSchema(
									&openapi.Schema{
										Format: "json",
										Type:   &openapi.Types{openapi.TypeString},
									},
									[]string{"application/json"},
								),
								Description: openapi.Ptr("The JWT key set"),
							}),
						),
					},
					Summary: "The JWT key set",
				},
			},
		},
		Type: ko.SystemPathType,
	}
}

func (hh *HostHandler) loginHandler(mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	if hh.authConfig == nil || hh.authExchanger == nil {
		return
	}

	const loginPath = "/~/login"
	mux.HandleFunc(
		"GET "+loginPath,
		func(w http.ResponseWriter, r *http.Request) {
			query := r.URL.Query()
			returnURL := query.Get("return")
			if returnURL == "" {
				returnURL = "/"
			}

			// If OIDC is configured, force login through it
			if authCodeURL := hh.authExchanger.AuthCodeURL(returnURL); authCodeURL != "" {
				http.Redirect(w, r, authCodeURL, http.StatusSeeOther)
				return
			}

			// Fallback: Local Login Page
			l, err := kdexhttp.GetLang(r, hh.defaultLanguage, hh.Translations.Languages())
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			rendered := hh.renderUtilityPage(
				kdexv1alpha1.LoginUtilityPageType,
				l,
				map[string]any{},
				&hh.Translations,
			)

			if rendered == "" {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}

			hh.log.V(1).Info("serving login page", "language", l.String())

			w.Header().Set("Content-Language", l.String())
			w.Header().Set("Content-Type", "text/html")

			_, err = w.Write([]byte(rendered))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		},
	)
	mux.HandleFunc(
		"POST "+loginPath,
		func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseForm(); err != nil {
				http.Error(w, "failed to parse form", http.StatusBadRequest)
				return
			}

			username := r.FormValue("username")
			password := r.FormValue("password")
			returnURL := r.FormValue("return")

			if returnURL == "" {
				returnURL = "/"
			}

			hh.log.V(1).Info("processing local login", "user", username)

			issuer := hh.serverAddress(r)

			token, err := hh.authExchanger.LoginLocal(r.Context(), issuer, username, password)
			if err != nil {
				// FAILED: 401 Unauthorized / render login page again with error message?
				// For now simple redirect back to login
				hh.log.Error(err, "local login failed")
				http.Redirect(w, r, "/~/login?error=invalid_credentials&return="+url.QueryEscape(returnURL), http.StatusSeeOther)
				return
			}

			// SUCCESS: Set cookie and redirect
			http.SetCookie(w, &http.Cookie{
				Name:     hh.authConfig.CookieName,
				Value:    token,
				Path:     "/",
				HttpOnly: true,
				Secure:   hh.isSecure(r),
				SameSite: http.SameSiteLaxMode,
			})

			http.Redirect(w, r, returnURL, http.StatusSeeOther)
		},
	)

	const logoutPath = "/~/logout"
	mux.HandleFunc(
		"POST "+logoutPath,
		func(w http.ResponseWriter, r *http.Request) {
			returnURL := "/"
			refURL, _ := url.Parse(r.Header.Get("Referer"))
			if refURL.Host == r.Host {
				returnURL = refURL.Path
			}

			// Clear local cookies
			http.SetCookie(w, &http.Cookie{
				Name:     hh.authConfig.CookieName,
				Value:    "",
				Path:     "/",
				MaxAge:   -1, // Tells browser to delete immediately
				HttpOnly: true,
				Secure:   hh.isSecure(r),
				SameSite: http.SameSiteLaxMode,
			})

			// Build the OIDC Logout URL
			logoutURLString, err := hh.authExchanger.EndSessionURL()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			if logoutURLString != "" {
				// Get the ID Token from the user's session
				idToken, err := hh.getAndDecryptToken(r, "oidc_hint")
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}

				logoutURL, err := url.Parse(logoutURLString)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}

				returnURL := fmt.Sprintf("%s%s", hh.serverAddress(r), returnURL)

				q := logoutURL.Query()
				q.Add("id_token_hint", idToken)
				q.Add("post_logout_redirect_uri", returnURL)
				logoutURL.RawQuery = q.Encode()

				// 4. Redirect the user's browser to the OIDC Provider
				http.Redirect(w, r, logoutURL.String(), http.StatusFound)
			} else {
				http.Redirect(w, r, returnURL, http.StatusFound)
			}
		},
	)

	hh.registerPath(loginPath, ko.PathInfo{
		API: ko.OpenAPI{
			BasePath: loginPath,
			Paths: map[string]ko.PathItem{
				loginPath: {
					Description: "Provides a localized login page.",
					Get: &openapi.Operation{
						Parameters: ko.ExtractParameters(loginPath, "return=foo", http.Header{}),
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
							openapi.WithName("400", &openapi.Response{
								Description: openapi.Ptr("Unable to ascertain the locale from the {l10n} parameter"),
							}),
							openapi.WithName("404", &openapi.Response{
								Description: openapi.Ptr("Resource not found"),
							}),
							openapi.WithName("500", &openapi.Response{
								Description: openapi.Ptr("Internal server error"),
							}),
						),
					},
					Post: &openapi.Operation{
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
							openapi.WithName("400", &openapi.Response{
								Description: openapi.Ptr("Unable to ascertain the locale from the {l10n} parameter"),
							}),
							openapi.WithName("404", &openapi.Response{
								Description: openapi.Ptr("Resource not found"),
							}),
							openapi.WithName("500", &openapi.Response{
								Description: openapi.Ptr("Internal server error"),
							}),
						),
					},
					Summary: "HTML login page",
				},
			},
		},
		Type: ko.SystemPathType,
	}, registeredPaths)
}

func (hh *HostHandler) navigationHandler(mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	const path = "/~/navigation/{navKey}/{l10n}/{basePathMinusLeadingSlash...}"
	mux.HandleFunc(
		"GET "+path,
		func(w http.ResponseWriter, r *http.Request) {
			hh.mu.RLock()
			defer hh.mu.RUnlock()

			basePath := "/" + r.PathValue("basePathMinusLeadingSlash")
			l10n := r.PathValue("l10n")
			navKey := r.PathValue("navKey")

			hh.log.V(2).Info("generating navigation", "basePath", basePath, "l10n", l10n, "navKey", navKey)

			var pageHandler *page.PageHandler

			for _, ph := range hh.Pages.List() {
				if ph.BasePath() == basePath {
					pageHandler = &ph
					break
				}
			}

			if pageHandler == nil {
				http.Error(w, "page not found", http.StatusNotFound)
				return
			}

			var nav string

			for key, n := range pageHandler.Navigations {
				if key == navKey {
					nav = n
					break
				}
			}

			if nav == "" {
				http.Error(w, "navigation not found", http.StatusNotFound)
				return
			}

			l, err := kdexhttp.GetLang(r, hh.defaultLanguage, hh.Translations.Languages())
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			// Filter navigation by page access checks

			rootEntry := &render.PageEntry{}
			hh.BuildMenuEntries(r.Context(), rootEntry, &l, l.String() == hh.defaultLanguage, nil)
			pageMap := *rootEntry.Children

			claims, _ := auth.GetClaims(r.Context())
			extra := map[string]any{}
			if claims != nil {
				extra["Identity"] = claims
			}

			renderer := render.Renderer{
				BasePath:        pageHandler.Page.BasePath,
				BrandName:       hh.host.BrandName,
				DefaultLanguage: hh.defaultLanguage,
				Extra:           extra,
				Language:        l.String(),
				Languages:       hh.availableLanguages(&hh.Translations),
				LastModified:    time.Now(),
				MessagePrinter:  hh.messagePrinter(&hh.Translations, l),
				Organization:    hh.host.Organization,
				PageMap:         pageMap,
				PatternPath:     pageHandler.PatternPath(),
				Title:           pageHandler.Label(),
			}

			templateData, err := renderer.TemplateData()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			rendered, err := renderer.RenderOne(navKey, nav, templateData)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "text/html")
			_, err = w.Write([]byte(rendered))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		},
	)

	hh.registerPath(path, ko.PathInfo{
		API: ko.OpenAPI{
			BasePath: path,
			Paths: map[string]ko.PathItem{
				path: {
					Description: "Provides dynamic HTML fragments for page navigation components, supporting localization and breadcrumb contexts.",
					Get: &openapi.Operation{
						Parameters: ko.ExtractParameters(path, "", http.Header{}),
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
							openapi.WithName("400", &openapi.Response{
								Description: openapi.Ptr("Unable to ascertain the locale from the {l10n} parameter"),
							}),
							openapi.WithName("404", &openapi.Response{
								Description: openapi.Ptr("Resource not found"),
							}),
							openapi.WithName("500", &openapi.Response{
								Description: openapi.Ptr("Internal server error"),
							}),
						),
					},
					Summary: "Dynamic Navigation Fragment Provider",
				},
			},
		},
		Type: ko.SystemPathType,
	}, registeredPaths)
}

func (hh *HostHandler) oauthHandler(mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	if hh.authConfig == nil || hh.authConfig.OIDCProviderURL == "" || hh.authExchanger == nil {
		return
	}

	const path = "/~/oauth/callback"
	mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")

		if code == "" {
			http.Error(w, "No code in request", http.StatusBadRequest)
			return
		}

		// Exchange code for ID Token
		rawIDToken, err := hh.authExchanger.ExchangeCode(r.Context(), code)
		if err != nil {
			hh.log.Error(err, "failed to exchange oauth code")
			http.Error(w, "Failed to exchange token", http.StatusUnauthorized)
			return
		}

		issuer := hh.serverAddress(r)

		// Exchange ID Token for Local Token
		localToken, err := hh.authExchanger.ExchangeToken(r.Context(), issuer, rawIDToken)
		if err != nil {
			hh.log.Error(err, "failed to exchange for local token")
			http.Error(w, "Failed to exchange for local token", http.StatusUnauthorized)
			return
		}

		options := &http.Cookie{
			Path:     "/",
			HttpOnly: true,
			Secure:   hh.isSecure(r),
			SameSite: http.SameSiteLaxMode,
			MaxAge:   3600, // 1 hour
		}

		if err := hh.encryptAndSplit(w, r, "oidc_hint", rawIDToken, options); err != nil {
			hh.log.Error(err, "failed to encrypt and split oidc token")
			http.Error(w, "Failed to store session hint", http.StatusInternalServerError)
			return
		}

		// Set Cookie
		http.SetCookie(w, &http.Cookie{
			Name:     hh.authConfig.CookieName,
			Value:    localToken,
			Path:     "/",
			HttpOnly: true,
			Secure:   hh.isSecure(r),
			SameSite: http.SameSiteLaxMode,
		})

		// Validate state/redirect
		redirectURL := state
		if redirectURL == "" || !strings.HasPrefix(redirectURL, "/") {
			redirectURL = "/"
		}

		http.Redirect(w, r, redirectURL, http.StatusSeeOther)
	})

	hh.registerPath(path, ko.PathInfo{
		API: ko.OpenAPI{
			BasePath: path,
			Paths: map[string]ko.PathItem{
				path: {
					Description: "OAuth2 Callback Endpoint",
					Get: &openapi.Operation{
						Parameters: ko.ExtractParameters(path, "code=foo&state=bar", http.Header{}),
						Responses: openapi.NewResponses(
							openapi.WithName("303", &openapi.Response{
								Description: openapi.Ptr("Redirect to original URL after successful login"),
							}),
							openapi.WithName("401", &openapi.Response{
								Description: openapi.Ptr("Unauthorized"),
							}),
						),
					},
					Summary: "OAuth2 Callback",
				},
			},
		},
		Type: ko.SystemPathType,
	}, registeredPaths)
}

func (hh *HostHandler) openapiHandler(mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	const path = "/~/openapi"

	// Register the path itself so it appears in the spec
	hh.registerPath(path, ko.PathInfo{
		API: ko.OpenAPI{
			BasePath: path,
			Paths: map[string]ko.PathItem{
				path: {
					Description: "Serves the generated OpenAPI 3.0 specification for this host.",
					Get: &openapi.Operation{
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
							openapi.WithName("500", &openapi.Response{
								Description: openapi.Ptr("Failed to marshal OpenAPI spec"),
							}),
						),
					},
					Summary: "OpenAPI Specification",
				},
			},
		},
		Type: ko.SystemPathType,
	}, registeredPaths)

	mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
		hh.mu.RLock()
		defer hh.mu.RUnlock()

		spec := hh.openapiBuilder.BuildOpenAPI(ko.Host(r), hh.Name, hh.registeredPaths, filterFromQuery(r.URL.Query()))

		jsonBytes, err := json.Marshal(spec)
		if err != nil {
			http.Error(w, "Failed to marshal OpenAPI spec", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, err = w.Write(jsonBytes)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
}

func (hh *HostHandler) pageHandlerFunc(
	basePath string,
	name string,
	l10nRenders map[string]string,
	pageHandler page.PageHandler,
) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if hh.authConfig != nil {
			// Check authorization before processing the request

			// Perform authorization check
			authorized, err := hh.authChecker.CheckAccess(
				r.Context(), "pages", basePath, hh.pageRequirements(&pageHandler))

			if err != nil {
				hh.log.Error(err, "authorization check failed", "page", name, "basePath", basePath)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			// User is not authorized
			if !authorized {
				hh.log.V(1).Info("unauthorized access attempt", "page", name, "basePath", basePath)

				// But is logged in, error page
				if _, isLoggedIn := auth.GetClaims(r.Context()); isLoggedIn {
					r.Header.Set("X-KDex-Sniffer-Skip", "true")
					http.Error(w, http.StatusText(http.StatusNotFound)+" "+r.URL.Path, http.StatusNotFound)
					return
				}

				// Redirect to login with return URL
				returnURL := r.URL.Path
				if r.URL.RawQuery != "" {
					returnURL += "?" + r.URL.RawQuery
				}
				redirectURL := "/~/login?return=" + url.QueryEscape(returnURL)
				http.Redirect(w, r, redirectURL, http.StatusSeeOther)
				return
			}
		}

		l, err := kdexhttp.GetLang(r, hh.defaultLanguage, hh.Translations.Languages())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		rendered, ok := l10nRenders[l.String()]

		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		hh.log.V(1).Info("serving", "page", name, "basePath", basePath, "language", l.String())

		w.Header().Set("Content-Language", l.String())
		w.Header().Set("Content-Type", "text/html")

		_, err = w.Write([]byte(rendered))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func (hh *HostHandler) schemaHandler(mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	const path = "/~/schema/{path...}"

	type schemaEntry struct {
		name   string
		path   string
		schema *openapi.SchemaRef
	}

	// Register the path itself so it appears in the spec
	hh.registerPath(path, ko.PathInfo{
		API: ko.OpenAPI{
			BasePath: path,
			Paths: map[string]ko.PathItem{
				path: {
					Description: "Serves individual JSONschema fragments from the registered OpenAPI specifications. The path should be in the format /~/schema/{basePath}/{schemaName} (e.g., /~/schema/v1/users/User) or simply /~/schema/{schemaName} for a global lookup.",
					Get: &openapi.Operation{
						Parameters: ko.ExtractParameters(path, "", http.Header{}),
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
							openapi.WithName("404", &openapi.Response{
								Description: openapi.Ptr("Schema not found"),
							}),
							openapi.WithName("500", &openapi.Response{
								Description: openapi.Ptr("Internal server error"),
							}),
						),
					},
					Summary: "JSONschema Fragment Provider",
				},
			},
		},
		Type: ko.SystemPathType,
	}, registeredPaths)

	mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
		requested := r.PathValue("path")

		hh.mu.RLock()
		defer hh.mu.RUnlock()

		orderedSchemaArray := []schemaEntry{}

		for path, info := range hh.registeredPaths {
			for name, schema := range info.API.Schemas {
				orderedSchemaArray = append(orderedSchemaArray, schemaEntry{
					name:   name,
					path:   path,
					schema: schema,
				})
			}
		}

		sort.Slice(orderedSchemaArray, func(i, j int) bool {
			if orderedSchemaArray[i].name < orderedSchemaArray[j].name {
				return true
			}
			return orderedSchemaArray[i].path < orderedSchemaArray[j].path
		})

		var foundSchema *openapi.SchemaRef

		// 1. Global lookup by schema name
		for _, schemaEntry := range orderedSchemaArray {
			if schemaEntry.name == requested {
				foundSchema = schemaEntry.schema
				break
			}
		}

		// 2. Namespaced lookup if global failed: /~/schema/{basePath}/{schemaName}
		if foundSchema == nil {
			fullPath := "/" + requested
			var bestMatchPath string
			var bestMatchSchema *openapi.SchemaRef

			for _, schemaEntry := range orderedSchemaArray {
				if fullPath == (schemaEntry.path + "/" + schemaEntry.name) {
					bestMatchPath = schemaEntry.path
					bestMatchSchema = schemaEntry.schema
				}
			}

			if bestMatchPath != "" {
				foundSchema = bestMatchSchema
			}
		}

		if foundSchema == nil {
			http.Error(w, "Schema not found", http.StatusNotFound)
			return
		}

		jsonBytes, err := json.Marshal(foundSchema)
		if err != nil {
			http.Error(w, "Failed to marshal schema", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, err = w.Write(jsonBytes)
		if err != nil {
			hh.log.Error(err, "failed to write schema response")
		}
	})
}

func (hh *HostHandler) snifferHandler(mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	if hh.sniffer != nil {
		// Register Inspect Handler
		mux.HandleFunc("/~/sniffer/inspect/{uuid}", hh.InspectHandler)

		const path = "/~/sniffer/docs"
		mux.HandleFunc("GET "+path, hh.sniffer.DocsHandler)
		registeredPaths[path] = ko.PathInfo{
			API: ko.OpenAPI{
				BasePath: path,
				Paths: map[string]ko.PathItem{
					path: {
						Description: "Provides Markdown documentation for the Request Sniffer's supported headers and behaviors.",
						Get: &openapi.Operation{
							Parameters: ko.ExtractParameters(path, "", http.Header{}),
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
							),
						},
						Summary: "Request Sniffer Documentation",
					},
				},
			},
			Type: ko.SystemPathType,
		}
	}
}

func (hh *HostHandler) stateHandler(mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	const path = "/~/state/"
	mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
		claims, ok := auth.GetClaims(r.Context())
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(claims); err != nil {
			hh.log.Error(err, "failed to encode claims")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})

	hh.registerPath(path, ko.PathInfo{
		API: ko.OpenAPI{
			BasePath: path,
			Paths: map[string]ko.PathItem{
				path: {
					Description: "Returns the current authenticated session state (claims) without requiring the client to parse the JWT.",
					Get: &openapi.Operation{
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
							openapi.WithName("401", &openapi.Response{
								Description: openapi.Ptr("User is not authenticated"),
							}),
						),
					},
					Summary: "Session State",
				},
			},
		},
		Type: ko.SystemPathType,
	}, registeredPaths)
}

func (hh *HostHandler) translationHandler(mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	const path = "/~/translation/{l10n}"
	mux.HandleFunc(
		"GET "+path,
		func(w http.ResponseWriter, r *http.Request) {
			hh.mu.RLock()
			defer hh.mu.RUnlock()

			l, err := kdexhttp.GetLang(r, hh.defaultLanguage, hh.Translations.Languages())
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			// Get all the keys and values for the given language
			keys := hh.Translations.Keys()
			// check query parameters for array of keys
			queryParams := r.URL.Query()
			keyParams := queryParams["key"]
			if len(keyParams) > 0 {
				keys = keyParams
			}

			keysAndValues := map[string]string{}
			printer := hh.messagePrinter(&hh.Translations, l)
			for _, key := range keys {
				keysAndValues[key] = printer.Sprintf(key)
				// replace each occurrence of the string `%!s(MISSING)` with a placeholder `{{n}}` where `n` is the alphabetic index of the placeholder
				parts := strings.Split(keysAndValues[key], "%!s(MISSING)")
				if len(parts) > 1 {
					var builder strings.Builder
					for i, part := range parts {
						builder.WriteString(part)
						if i < len(parts)-1 {
							// Convert index to alphabetic character (0 -> a, 1 -> b, etc.)
							placeholder := 'a' + i
							if placeholder > 'z' {
								// Fallback or handle wrap if more than 26 placeholders are present
								fmt.Fprintf(&builder, "{{%d}}", i)
							} else {
								fmt.Fprintf(&builder, "{{%c}}", placeholder)
							}
						}
					}
					keysAndValues[key] = builder.String()
				}
			}

			w.Header().Set("Content-Type", "application/json")
			err = json.NewEncoder(w).Encode(keysAndValues)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		},
	)

	op := &openapi.Operation{
		Parameters: ko.ExtractParameters(path, "key=one&key=two", http.Header{}),
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
			openapi.WithName("500", &openapi.Response{
				Description: openapi.Ptr("Internal server error"),
			}),
		),
	}

	hh.registerPath(path, ko.PathInfo{
		API: ko.OpenAPI{
			BasePath: path,
			Paths: map[string]ko.PathItem{
				path: {
					Description: "Provides a JSON map of localization keys and their translated values for a given language tag.",
					Get:         op,
					Summary:     "Localization Key Provider",
				},
			},
		},
		Type: ko.SystemPathType,
	}, registeredPaths)
}

func (hh *HostHandler) unimplementedHandler(pattern string, mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	mux.HandleFunc(
		pattern,
		func(w http.ResponseWriter, r *http.Request) {
			hh.log.V(1).Info("unimplemented handler", "path", r.URL.Path)
			err := fmt.Errorf(`{"path": "%s", "message": "Nothing here yet..."}`, r.URL.Path)
			http.Error(w, err.Error(), http.StatusNotImplemented)
		},
	)

	parts := strings.Split(pattern, " ")
	path := pattern
	if len(parts) > 1 {
		path = parts[1]
	}

	info := ko.PathInfo{
		API: ko.OpenAPI{
			BasePath: path,
			Paths: map[string]ko.PathItem{
				path: {
					Description: fmt.Sprintf("Internal system endpoint providing %s functionality. NOT YET IMPLEMENTED!", path),
					Get: &openapi.Operation{
						Parameters: ko.ExtractParameters(path, "", http.Header{}),
						Responses: openapi.NewResponses(
							openapi.WithName("501", &openapi.Response{
								Description: openapi.Ptr("Not Implemented - This system endpoint is defined but not yet functional."),
							}),
						),
					},
					Summary: fmt.Sprintf("System Endpoint: %s", path),
				},
			},
		},
		Type: ko.SystemPathType,
	}

	hh.registerPath(path, info, registeredPaths)
}
