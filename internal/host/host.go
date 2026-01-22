package host

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	openapi "github.com/getkin/kin-openapi/openapi3"
	"github.com/go-logr/logr"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/crds/render"
	"kdex.dev/web/internal/host/ico"
	kdexhttp "kdex.dev/web/internal/http"
	ko "kdex.dev/web/internal/openapi"
	"kdex.dev/web/internal/page"
	"kdex.dev/web/internal/sniffer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NewHostHandler(c client.Client, name string, namespace string, log logr.Logger) *HostHandler {
	th := &HostHandler{
		Name:                      name,
		Namespace:                 namespace,
		client:                    c,
		defaultLanguage:           "en",
		log:                       log.WithValues("host", name),
		translationResources:      map[string]kdexv1alpha1.KDexTranslationSpec{},
		utilityPages:              map[kdexv1alpha1.KDexUtilityPageType]page.PageHandler{},
		registeredPaths:           map[string]ko.PathInfo{},
		pathsCollectedInReconcile: map[string]ko.PathInfo{},
		analysisCache:             NewAnalysisCache(),
	}

	translations, err := NewTranslations(th.defaultLanguage, map[string]kdexv1alpha1.KDexTranslationSpec{})
	if err != nil {
		panic(err)
	}

	th.Translations = *translations
	th.Pages = page.NewPageStore(
		name,
		th.RebuildMux,
		th.log.WithName("pages"),
	)
	th.RebuildMux()
	return th
}

func (th *HostHandler) AddOrUpdateTranslation(name string, translation *kdexv1alpha1.KDexTranslationSpec) {
	if translation == nil {
		return
	}
	th.log.V(1).Info("add or update translation", "translation", name)
	th.mu.Lock()
	th.translationResources[name] = *translation
	th.mu.Unlock()
	th.RebuildMux() // Called after lock is released
}

func (th *HostHandler) AddOrUpdateUtilityPage(ph page.PageHandler) {
	if ph.UtilityPage == nil {
		return
	}
	th.log.V(1).Info("add or update utility page", "name", ph.Name, "type", ph.UtilityPage.Type)
	th.mu.Lock()
	th.utilityPages[ph.UtilityPage.Type] = ph
	th.mu.Unlock()
	th.RebuildMux()
}

func (th *HostHandler) FootScriptToHTML(handler page.PageHandler) string {
	var buffer bytes.Buffer
	separator := ""

	for _, script := range th.scripts {
		buffer.WriteString(separator)
		buffer.WriteString(script.ToFootTag())
		separator = "\n"
	}
	for _, script := range handler.Scripts {
		buffer.WriteString(separator)
		buffer.WriteString(script.ToFootTag())
		separator = "\n"
	}

	return buffer.String()
}

func (th *HostHandler) GetUtilityPageHandler(name kdexv1alpha1.KDexUtilityPageType) page.PageHandler {
	th.mu.RLock()
	defer th.mu.RUnlock()
	ph, ok := th.utilityPages[name]
	if !ok {
		return page.PageHandler{}
	}
	return ph
}

func (th *HostHandler) HeadScriptToHTML(handler page.PageHandler) string {
	packageReferences := []kdexv1alpha1.PackageReference{}
	packageReferences = append(packageReferences, th.packageReferences...)
	packageReferences = append(packageReferences, handler.PackageReferences...)

	var buffer bytes.Buffer
	separator := ""

	if len(packageReferences) > 0 {
		buffer.WriteString("<script type=\"importmap\">\n")
		buffer.WriteString(th.importmap)
		buffer.WriteString("\n</script>\n")

		buffer.WriteString("<script type=\"module\">\n")
		for _, pr := range packageReferences {
			buffer.WriteString(separator)
			buffer.WriteString(pr.ToImportStatement())
			separator = "\n"
		}
		buffer.WriteString("\n</script>")
	}

	for _, script := range th.scripts {
		buffer.WriteString(separator)
		buffer.WriteString(script.ToHeadTag())
		separator = "\n"
	}
	for _, script := range handler.Scripts {
		buffer.WriteString(separator)
		buffer.WriteString(script.ToHeadTag())
		separator = "\n"
	}

	return buffer.String()
}

func (th *HostHandler) L10nRender(
	handler page.PageHandler,
	pageMap map[string]any,
	l language.Tag,
	extraTemplateData map[string]any,
	translations *Translations,
) (string, error) {

	// make sure everything passed to the renderer is mutation safe (i.e. copy it)

	renderer := render.Renderer{
		BasePath:        handler.BasePath(),
		BrandName:       th.host.BrandName,
		Contents:        handler.ContentToHTMLMap(),
		DefaultLanguage: th.defaultLanguage,
		Extra:           maps.Clone(extraTemplateData),
		Footer:          handler.Footer,
		FootScript:      th.FootScriptToHTML(handler),
		Header:          handler.Header,
		HeadScript:      th.HeadScriptToHTML(handler),
		Language:        l.String(),
		Languages:       th.availableLanguages(translations),
		LastModified:    time.Now(),
		MessagePrinter:  th.messagePrinter(translations, l),
		Meta:            th.MetaToString(handler, l),
		Navigations:     handler.NavigationToHTMLMap(),
		Organization:    th.host.Organization,
		PageMap:         maps.Clone(pageMap),
		PatternPath:     handler.PatternPath(),
		TemplateContent: handler.MainTemplate,
		TemplateName:    handler.Name,
		Theme:           th.ThemeAssetsToString(),
		Title:           handler.Label(),
	}

	return renderer.RenderPage()
}

func (th *HostHandler) L10nRenders(
	handler page.PageHandler,
	pageMaps map[language.Tag]map[string]any,
	translations *Translations,
) map[string]string {
	l10nRenders := make(map[string]string)
	for _, l := range translations.Languages() {
		rendered, err := th.L10nRender(handler, pageMaps[l], l, map[string]any{}, translations)
		if err != nil {
			th.log.Error(err, "failed to render page for language", "page", handler.Name, "language", l)
			continue
		}
		l10nRenders[l.String()] = rendered
	}
	return l10nRenders
}

func (th *HostHandler) MetaToString(handler page.PageHandler, l language.Tag) string {
	var buffer bytes.Buffer

	if len(th.host.Assets) > 0 {
		buffer.WriteString(th.host.Assets.String())
		buffer.WriteRune('\n')
	}

	basePath := handler.BasePath()
	if l.String() != th.defaultLanguage {
		basePath = "/" + l.String() + basePath
	}
	patternPath := handler.PatternPath()
	if l.String() != th.defaultLanguage {
		patternPath = "/" + l.String() + patternPath
	}

	fmt.Fprintf(
		&buffer,
		kdexUIMetaTemplate,
		basePath,
		patternPath,
	)

	// data-check-batch-endpoint="/~/check/batch"
	// data-check-single-endpoint="/~/check/single"
	// data-login-path="/~/oauth/login"
	// data-login-label="Login"
	// data-login-css-query="nav.nav .nav-dropdown a.login"
	// data-logout-path="/~/oauth/logout"
	// data-logout-label="Logout"
	// data-logout-css-query="nav.nav .nav-dropdown a.logout"
	// data-path-separator="/_/"
	// data-state-endpoint="/~/state/out"

	return buffer.String()
}

func (th *HostHandler) RebuildMux() {
	th.log.V(1).Info("rebuilding mux")
	th.mu.RLock()

	if th.host == nil {
		th.mu.RUnlock()
		return
	}

	// copy fields that we need while under RLock
	defaultLanguageResource := th.defaultLanguage
	translationResources := maps.Clone(th.translationResources)

	newTranslations, err := NewTranslations(defaultLanguageResource, translationResources)
	if err != nil {
		th.log.Error(err, "failed to rebuild translations")
		th.mu.RUnlock()
		return
	}

	registeredPaths := map[string]ko.PathInfo{}
	maps.Copy(registeredPaths, th.pathsCollectedInReconcile)

	mux := th.muxWithDefaultsLocked(registeredPaths)

	pageHandlers := th.Pages.List()

	if len(pageHandlers) == 0 {
		handler := func(w http.ResponseWriter, r *http.Request) {
			l, err := kdexhttp.GetLang(r, th.defaultLanguage, th.Translations.Languages())
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			rendered := th.renderUtilityPage(
				kdexv1alpha1.AnnouncementUtilityPageType,
				l,
				map[string]any{},
				&th.Translations,
			)

			if rendered == "" {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}

			th.log.V(1).Info("serving announcement page", "language", l.String())

			w.Header().Set("Content-Language", l.String())
			w.Header().Set("Content-Type", "text/html")

			_, err = w.Write([]byte(rendered))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		}

		mux.HandleFunc("GET /{$}", handler)
		mux.HandleFunc("GET /{l10n}/{$}", handler)

		th.mu.RUnlock()
		th.mu.Lock()
		th.Translations = *newTranslations
		th.registeredPaths = registeredPaths
		th.Mux = mux
		th.mu.Unlock()

		return
	}

	renderedPages := []pageRender{}

	for _, ph := range pageHandlers {
		basePath := ph.BasePath()

		if basePath == "" {
			th.log.V(1).Info("somehow page has empty basePath, skipping", "page", ph.Name)
			continue
		}

		l10nRenders := th.L10nRenders(ph, nil, newTranslations)
		renderedPages = append(renderedPages, pageRender{ph: ph, l10nRenders: l10nRenders})
	}

	th.mu.RUnlock()

	for _, pr := range renderedPages {
		th.addHandlerAndRegister(mux, pr, registeredPaths)
	}

	th.mu.Lock()
	th.Translations = *newTranslations
	th.registeredPaths = registeredPaths
	th.Mux = mux
	th.mu.Unlock()
}

func (th *HostHandler) RemoveTranslation(name string) {
	th.log.V(1).Info("delete translation", "translation", name)
	th.mu.Lock()
	delete(th.translationResources, name)
	th.mu.Unlock()

	th.RebuildMux() // Called after lock is released
}

func (th *HostHandler) RemoveUtilityPage(name string) {
	th.log.V(1).Info("delete utility page", "name", name)
	th.mu.Lock()
	for t, ph := range th.utilityPages {
		if ph.Name == name {
			delete(th.utilityPages, t)
			break
		}
	}
	th.mu.Unlock()

	th.RebuildMux() // Called after lock is released
}

func (th *HostHandler) SecurityModes() []string {
	// For now we assume that if a login page is specified we want to default to bearer auth
	// as the preferred mode of authentication for auto-generated functions.
	if th.host != nil && th.host.UtilityPages != nil && th.host.UtilityPages.LoginRef != nil {
		return []string{"bearer"}
	}

	// Fallback to bearer if we have a host at all, better than nothing for signaling auth intent.
	if th.host != nil {
		return []string{"bearer"}
	}

	return []string{}
}

func (th *HostHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	th.mu.RLock()
	mux := th.Mux
	th.mu.RUnlock()

	if mux == nil {
		th.serveError(w, r, http.StatusNotFound, "not found")
		return
	}

	wrappedMux := th.DesignMiddleware(mux)
	wrappedMux.ServeHTTP(w, r)
}

func (th *HostHandler) SetHost(
	host *kdexv1alpha1.KDexHostSpec,
	packageReferences []kdexv1alpha1.PackageReference,
	themeAssets []kdexv1alpha1.Asset,
	scripts []kdexv1alpha1.ScriptDef,
	importmap string,
	paths map[string]ko.PathInfo,
	functions []kdexv1alpha1.KDexFunction,
) {
	th.mu.Lock()
	th.host = host
	th.defaultLanguage = host.DefaultLang
	th.favicon = ico.NewICO(host.FaviconSVGTemplate, render.TemplateData{
		BrandName:       host.BrandName,
		DefaultLanguage: host.DefaultLang,
		Organization:    host.Organization,
	})
	th.packageReferences = packageReferences
	th.pathsCollectedInReconcile = paths
	th.themeAssets = themeAssets
	th.scripts = scripts

	var snif *sniffer.RequestSniffer
	if host.DevMode {
		snif = &sniffer.RequestSniffer{
			BasePathRegex: (&kdexv1alpha1.API{}).BasePathRegex(),
			Client:        th.client,
			Functions:     functions,
			HostName:      th.Name,
			ItemPathRegex: (&kdexv1alpha1.API{}).ItemPathRegex(),
			Namespace:     th.Namespace,
			SecurityModes: th.SecurityModes(),
		}
	}

	th.sniffer = snif
	th.importmap = importmap
	th.mu.Unlock()
	th.RebuildMux()
}

func (th *HostHandler) ThemeAssetsToString() string {
	var buffer bytes.Buffer

	for _, asset := range th.themeAssets {
		buffer.WriteString(asset.ToTag())
		buffer.WriteRune('\n')
	}

	return buffer.String()
}

func (th *HostHandler) addHandlerAndRegister(mux *http.ServeMux, pr pageRender, registeredPaths map[string]ko.PathInfo) {
	finalPath := toFinalPath(pr.ph.BasePath())
	label := pr.ph.Label()

	handler := th.pageHandlerFunc(finalPath, pr.ph.Name, pr.l10nRenders)

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

	th.registerPath(finalPath, ko.PathInfo{
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
	th.registerPath(l10nPath, ko.PathInfo{
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

		th.registerPath(patternPath, ko.PathInfo{
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

		th.registerPath(l10nPatternPath, ko.PathInfo{
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

func (th *HostHandler) availableLanguages(translations *Translations) []string {
	availableLangs := []string{}

	for _, tag := range translations.Languages() {
		availableLangs = append(availableLangs, tag.String())
	}

	return availableLangs
}

func (th *HostHandler) faviconHandler(mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	const path = "/favicon.ico"
	mux.HandleFunc("GET "+path, th.favicon.FaviconHandler)
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
		Type: ko.InternalPathType,
	}
}

func (th *HostHandler) messagePrinter(translations *Translations, tag language.Tag) *message.Printer {
	return message.NewPrinter(
		tag,
		message.Catalog(translations.Catalog()),
	)
}

func (th *HostHandler) muxWithDefaultsLocked(registeredPaths map[string]ko.PathInfo) *http.ServeMux {
	mux := http.NewServeMux()

	th.faviconHandler(mux, registeredPaths)
	th.navigationHandler(mux, registeredPaths)
	th.translationHandler(mux, registeredPaths)
	th.snifferHandler(mux, registeredPaths)
	th.openapiHandler(mux, registeredPaths)

	// Register Inspect Handler
	mux.HandleFunc("/inspect/{uuid}", th.InspectHandler)

	// TODO: implement a state handler
	// TODO: implement an oauth handler
	// TODO: implement a check handler

	th.unimplementedHandler("GET /~/check/", mux, registeredPaths)
	th.unimplementedHandler("GET /~/oauth/", mux, registeredPaths)
	th.unimplementedHandler("GET /~/state/", mux, registeredPaths)

	return mux
}

func (th *HostHandler) navigationHandler(mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	const path = "/~/navigation/{navKey}/{l10n}/{basePathMinusLeadingSlash...}"
	mux.HandleFunc(
		"GET "+path,
		func(w http.ResponseWriter, r *http.Request) {
			th.mu.RLock()
			defer th.mu.RUnlock()

			basePath := "/" + r.PathValue("basePathMinusLeadingSlash")
			l10n := r.PathValue("l10n")
			navKey := r.PathValue("navKey")

			th.log.V(2).Info("generating navigation", "basePath", basePath, "l10n", l10n, "navKey", navKey)

			var pageHandler *page.PageHandler

			for _, ph := range th.Pages.List() {
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

			l, err := kdexhttp.GetLang(r, th.defaultLanguage, th.Translations.Languages())
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			rootEntry := &render.PageEntry{}
			th.Pages.BuildMenuEntries(rootEntry, &l, l.String() == th.defaultLanguage, nil)
			pageMap := *rootEntry.Children

			renderer := render.Renderer{
				BasePath:        pageHandler.Page.BasePath,
				BrandName:       th.host.BrandName,
				DefaultLanguage: th.defaultLanguage,
				Language:        l.String(),
				Languages:       th.availableLanguages(&th.Translations),
				LastModified:    time.Now(),
				MessagePrinter:  th.messagePrinter(&th.Translations, l),
				Organization:    th.host.Organization,
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

	th.registerPath(path, ko.PathInfo{
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
		Type: ko.InternalPathType,
	}, registeredPaths)
}

func (th *HostHandler) openapiHandler(mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	const path = "/~/openapi"

	// Register the path itself so it appears in the spec
	th.registerPath(path, ko.PathInfo{
		API: ko.OpenAPI{
			BasePath: path,
			Paths: map[string]ko.PathItem{
				path: {
					Description: "Serves the generated OpenAPI 3.0 specification for this host.",
					Get: &openapi.Operation{
						Parameters: ko.ExtractParameters(path, "path=one&path=two&tag=one&tag=two&type=one", http.Header{}),
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
		Type: ko.InternalPathType,
	}, registeredPaths)

	mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
		th.mu.RLock()
		defer th.mu.RUnlock()

		spec := ko.BuildOpenAPI(ko.Host(r), th.Name, th.registeredPaths, filterFromQuery(r.URL.Query()))

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

func (th *HostHandler) pageHandlerFunc(
	basePath string,
	name string,
	l10nRenders map[string]string,
) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		l, err := kdexhttp.GetLang(r, th.defaultLanguage, th.Translations.Languages())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		rendered, ok := l10nRenders[l.String()]

		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		th.log.V(1).Info("serving", "page", name, "basePath", basePath, "language", l.String())

		w.Header().Set("Content-Language", l.String())
		w.Header().Set("Content-Type", "text/html")

		_, err = w.Write([]byte(rendered))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func (th *HostHandler) registerPath(path string, info ko.PathInfo, m map[string]ko.PathInfo) {
	current, ok := m[path]
	if !ok {
		if info.API.BasePath == "" {
			info.API.BasePath = path
		}
		m[path] = info
		return
	}

	ko.MergeOperations(&current.API, &info.API)

	if current.API.BasePath == "" {
		current.API.BasePath = path
	}

	m[path] = current
}

func (th *HostHandler) renderUtilityPage(utilityType kdexv1alpha1.KDexUtilityPageType, l language.Tag, extraTemplateData map[string]any, translations *Translations) string {
	ph, ok := th.utilityPages[utilityType]
	if !ok {
		return ""
	}

	rendered, err := th.L10nRender(ph, map[string]any{}, l, extraTemplateData, translations)
	if err != nil {
		th.log.Error(err, "failed to render utility page", "page", ph.Name, "language", l)
		return ""
	}

	return rendered
}

func (th *HostHandler) serveError(w http.ResponseWriter, r *http.Request, code int, msg string) {
	th.mu.RLock()
	l, err := kdexhttp.GetLang(r, th.defaultLanguage, th.Translations.Languages())
	if err != nil {
		l = language.Make(th.defaultLanguage)
	}

	// collect stacktrace
	stacktrace := string(debug.Stack())

	th.log.V(2).Info("generating error page", "requestURI", r.URL.Path, "code", code, "msg", msg, "language", l, "stacktrace", stacktrace)

	rendered := th.renderUtilityPage(
		kdexv1alpha1.ErrorUtilityPageType,
		l,
		map[string]any{"ErrorCode": code, "ErrorCodeString": http.StatusText(code), "ErrorMessage": msg},
		&th.Translations,
	)
	th.mu.RUnlock()

	if rendered == "" {
		// Fallback to standard http error if no custom error page
		http.Error(w, msg, code)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Content-Language", l.String())
	w.WriteHeader(code)
	_, _ = w.Write([]byte(rendered))
}

func (th *HostHandler) snifferHandler(mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	if th.sniffer != nil {
		const path = "/~/sniffer/docs"
		mux.HandleFunc("GET "+path, th.sniffer.DocsHandler)
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
			Type: ko.InternalPathType,
		}
	}
}

func (th *HostHandler) translationHandler(mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	const path = "/~/translation/{l10n}"
	mux.HandleFunc(
		"GET "+path,
		func(w http.ResponseWriter, r *http.Request) {
			th.mu.RLock()
			defer th.mu.RUnlock()

			l, err := kdexhttp.GetLang(r, th.defaultLanguage, th.Translations.Languages())
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			// Get all the keys and values for the given language
			keys := th.Translations.Keys()
			// check query parameters for array of keys
			queryParams := r.URL.Query()
			keyParams := queryParams["key"]
			if len(keyParams) > 0 {
				keys = keyParams
			}

			keysAndValues := map[string]string{}
			printer := th.messagePrinter(&th.Translations, l)
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

	th.registerPath(path, ko.PathInfo{
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
		Type: ko.InternalPathType,
	}, registeredPaths)
}

func (th *HostHandler) unimplementedHandler(pattern string, mux *http.ServeMux, registeredPaths map[string]ko.PathInfo) {
	mux.HandleFunc(
		pattern,
		func(w http.ResponseWriter, r *http.Request) {
			th.log.V(1).Info("unimplemented handler", "path", r.URL.Path)
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
		Type: ko.InternalPathType,
	}

	th.registerPath(path, info, registeredPaths)
}

func filterFromQuery(queryParams url.Values) ko.Filter {
	filter := ko.Filter{}

	pathParams := queryParams["path"]
	if len(pathParams) > 0 {
		filter.Paths = pathParams
	}

	tagParams := queryParams["tag"]
	if len(tagParams) > 0 {
		filter.Tags = tagParams
	}

	typeParam := queryParams["type"]
	if len(typeParam) > 0 {
		t := strings.ToUpper(typeParam[0])

		switch t {
		case string(ko.BackendPathType):
			filter.Type = ko.BackendPathType
		case string(ko.FunctionPathType):
			filter.Type = ko.FunctionPathType
		case string(ko.InternalPathType):
			filter.Type = ko.InternalPathType
		case string(ko.PagePathType):
			filter.Type = ko.PagePathType
		}
	}

	return filter
}

func toFinalPath(path string) string {
	if !strings.HasSuffix(path, "/") {
		path = path + "/"
	}
	path = path + "{$}"
	return path
}
