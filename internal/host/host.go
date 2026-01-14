package host

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"regexp"
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
	"kdex.dev/web/internal/page"
)

func NewHostHandler(name string, namespace string, log logr.Logger) *HostHandler {
	th := &HostHandler{
		Name:                      name,
		Namespace:                 namespace,
		defaultLanguage:           "en",
		log:                       log.WithValues("host", name),
		translationResources:      map[string]kdexv1alpha1.KDexTranslationSpec{},
		utilityPages:              map[kdexv1alpha1.KDexUtilityPageType]page.PageHandler{},
		registeredPaths:           map[string]PathInfo{},
		pathsCollectedInReconcile: map[string]PathInfo{},
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

func (th *HostHandler) Domains() []string {
	th.mu.RLock()
	defer th.mu.RUnlock()
	if th.host == nil {
		return []string{}
	}
	return th.host.Routing.Domains
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

	registeredPaths := map[string]PathInfo{}
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

	type pageRender struct {
		ph          page.PageHandler
		l10nRenders map[string]string
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
		ph := pr.ph
		basePath := ph.BasePath()
		l10nRenders := pr.l10nRenders

		handler := func(w http.ResponseWriter, r *http.Request) {
			rend := l10nRenders
			name := ph.Name
			bp := basePath

			l, err := kdexhttp.GetLang(r, th.defaultLanguage, th.Translations.Languages())
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			rendered, ok := rend[l.String()]

			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}

			th.log.V(1).Info("serving", "page", name, "basePath", bp, "language", l.String())

			w.Header().Set("Content-Language", l.String())
			w.Header().Set("Content-Type", "text/html")

			_, err = w.Write([]byte(rendered))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		}

		finalPath := basePath
		if strings.HasSuffix(finalPath, "/") {
			finalPath = finalPath + "{$}"
		} else {
			finalPath = finalPath + "/{$}"
		}

		mux.HandleFunc("GET "+finalPath, handler)
		mux.HandleFunc("GET /{l10n}"+finalPath, handler)

		th.registerPath(finalPath, PathInfo{
			API: kdexv1alpha1.KDexOpenAPI{
				Description: fmt.Sprintf("Rendered HTML page for %s", ph.Label()),
				KDexOpenAPIInternal: kdexv1alpha1.KDexOpenAPIInternal{
					Get: &openapi.Operation{
						Parameters: th.extractParameters(finalPath, ""),
					},
				},
				Path:    finalPath,
				Summary: ph.Label(),
			},
			Type: PagePathType,
		}, registeredPaths)

		l10nPath := "/{l10n}" + finalPath
		th.registerPath(l10nPath, PathInfo{
			API: kdexv1alpha1.KDexOpenAPI{
				Description: fmt.Sprintf("Localized rendered HTML page for %s", ph.Label()),
				KDexOpenAPIInternal: kdexv1alpha1.KDexOpenAPIInternal{
					Get: &openapi.Operation{
						Parameters: th.extractParameters(l10nPath, ""),
					},
				},
				Path:    l10nPath,
				Summary: fmt.Sprintf("%s (Localized)", ph.Label()),
			},
			Type: PagePathType,
		}, registeredPaths)

		patternPath := ph.Page.PatternPath
		l10nPatternPath := "/{l10n}" + patternPath

		if patternPath != "" {
			mux.HandleFunc("GET "+patternPath, handler)
			mux.HandleFunc("GET "+l10nPatternPath, handler)

			th.registerPath(patternPath, PathInfo{
				API: kdexv1alpha1.KDexOpenAPI{
					Description: fmt.Sprintf("Rendered HTML page for %s using pattern %s", ph.Label(), patternPath),
					KDexOpenAPIInternal: kdexv1alpha1.KDexOpenAPIInternal{
						Get: &openapi.Operation{
							Parameters: th.extractParameters(patternPath, ""),
						},
					},
					Path:    patternPath,
					Summary: ph.Label(),
				},
				Type: PagePathType,
			}, registeredPaths)

			th.registerPath(l10nPatternPath, PathInfo{
				API: kdexv1alpha1.KDexOpenAPI{
					Description: fmt.Sprintf("Localized rendered HTML page for %s using pattern %s", ph.Label(), l10nPatternPath),
					KDexOpenAPIInternal: kdexv1alpha1.KDexOpenAPIInternal{
						Get: &openapi.Operation{
							Parameters: th.extractParameters(l10nPatternPath, ""),
						},
					},
					Path:    l10nPatternPath,
					Summary: fmt.Sprintf("%s (Localized)", ph.Label()),
				},
				Type: PagePathType,
			}, registeredPaths)
		}
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

	ew := &errorResponseWriter{ResponseWriter: w}
	mux.ServeHTTP(ew, r)

	if ew.statusCode >= 400 {
		if ew.statusCode == http.StatusNotFound && th.Sniffer != nil {
			w.Header().Set("X-KDex-Sniffer-Docs", "/~/sniffer/docs")
			if err := th.Sniffer.Sniff(r); err != nil {
				th.log.Error(err, "failed to sniff request", "path", r.URL.Path)
			}
		}
		th.serveError(w, r, ew.statusCode, ew.statusMsg)
	}
}

func (th *HostHandler) SetHost(
	host *kdexv1alpha1.KDexHostSpec,
	packageReferences []kdexv1alpha1.PackageReference,
	themeAssets []kdexv1alpha1.Asset,
	scripts []kdexv1alpha1.ScriptDef,
	importmap string,
	paths map[string]PathInfo,
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

func (th *HostHandler) availableLanguages(translations *Translations) []string {
	availableLangs := []string{}

	for _, tag := range translations.Languages() {
		availableLangs = append(availableLangs, tag.String())
	}

	return availableLangs
}

func (th *HostHandler) buildOpenAPI(registeredPaths map[string]PathInfo) *openapi.T {
	doc := &openapi.T{
		OpenAPI: "3.0.0",
		Info: &openapi.Info{
			Title:       fmt.Sprintf("KDex Host: %s", th.Name),
			Description: "Auto-generated OpenAPI specification for KDex Host",
			Version:     "1.0.0",
		},
		Paths: &openapi.Paths{},
	}

	for _, info := range registeredPaths {
		path := info.API.Path
		if path == "" {
			continue
		}

		pathItem := &openapi.PathItem{
			Summary:     info.API.Summary,
			Description: info.API.Description,
		}

		// Fill path item operations
		ensureOpMetadata := func(op *openapi.Operation) {
			if op == nil {
				return
			}
			if op.Summary == "" {
				op.Summary = info.API.Summary
			}
			if op.Description == "" {
				op.Description = info.API.Description
			}
		}

		if info.API.Get != nil {
			ensureOpMetadata(info.API.Get)
			pathItem.Get = info.API.Get
		}
		if info.API.Put != nil {
			ensureOpMetadata(info.API.Put)
			pathItem.Put = info.API.Put
		}
		if info.API.Post != nil {
			ensureOpMetadata(info.API.Post)
			pathItem.Post = info.API.Post
		}
		if info.API.Delete != nil {
			ensureOpMetadata(info.API.Delete)
			pathItem.Delete = info.API.Delete
		}
		if info.API.Options != nil {
			ensureOpMetadata(info.API.Options)
			pathItem.Options = info.API.Options
		}
		if info.API.Head != nil {
			ensureOpMetadata(info.API.Head)
			pathItem.Head = info.API.Head
		}
		if info.API.Patch != nil {
			ensureOpMetadata(info.API.Patch)
			pathItem.Patch = info.API.Patch
		}
		if info.API.Trace != nil {
			ensureOpMetadata(info.API.Trace)
			pathItem.Trace = info.API.Trace
		}
		if info.API.Connect != nil {
			ensureOpMetadata(info.API.Connect)
			pathItem.Connect = info.API.Connect
		}

		doc.Paths.Set(path, pathItem)
	}

	return doc
}

func (th *HostHandler) extractParameters(path string, query string) openapi.Parameters {
	var params openapi.Parameters

	// Regular expression to match path parameters: {name} or {name...}
	paramRegex := regexp.MustCompile(`\{([^}]+)\}`)
	matches := paramRegex.FindAllStringSubmatch(path, -1)

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		paramName := match[1]
		isWildcard := strings.HasSuffix(paramName, "...")

		// Clean the parameter name
		cleanName := strings.TrimSuffix(paramName, "...")

		// Create parameter description based on name
		description := fmt.Sprintf("Path parameter: %s", cleanName)
		if isWildcard {
			description = fmt.Sprintf("Wildcard path parameter: %s (captures remaining path segments)", cleanName)
		}

		// Determine schema type
		schema := openapi.NewStringSchema()
		if isWildcard {
			// Wildcard parameters can contain slashes and multiple segments
			schema.Description = "May contain multiple path segments separated by slashes"
		}

		param := &openapi.Parameter{
			Name:        cleanName,
			In:          "path",
			Description: description,
			Required:    true,
			Schema:      openapi.NewSchemaRef("", schema),
		}

		params = append(params, &openapi.ParameterRef{
			Value: param,
		})
	}

	// parse the query string for parameters
	if query != "" {
		// Track parameter occurrences to detect arrays
		paramCounts := make(map[string]int)

		// Parse query string manually to count occurrences
		pairs := strings.Split(query, "&")
		for _, pair := range pairs {
			if pair == "" {
				continue
			}

			parts := strings.SplitN(pair, "=", 2)
			if len(parts) > 0 {
				key := parts[0]
				paramCounts[key]++
			}
		}

		// Create parameters for unique keys
		for paramName, count := range paramCounts {
			isArray := count > 1

			description := fmt.Sprintf("Query parameter: %s", paramName)
			if isArray {
				description = fmt.Sprintf("Query parameter: %s (array - multiple values supported)", paramName)
			}

			// Determine schema type
			var schema *openapi.Schema
			if isArray {
				schema = openapi.NewArraySchema()
				schema.Items = openapi.NewSchemaRef("", openapi.NewStringSchema())
			} else {
				schema = openapi.NewStringSchema()
			}

			param := &openapi.Parameter{
				Name:        paramName,
				In:          "query",
				Description: description,
				Required:    false, // Query parameters are typically optional
				Schema:      openapi.NewSchemaRef("", schema),
			}

			params = append(params, &openapi.ParameterRef{
				Value: param,
			})
		}
	}

	return params
}

func (th *HostHandler) faviconHandler(mux *http.ServeMux, registeredPaths map[string]PathInfo) {
	const path = "/favicon.ico"
	mux.HandleFunc("GET "+path, th.favicon.FaviconHandler)
	registeredPaths[path] = PathInfo{
		API: kdexv1alpha1.KDexOpenAPI{
			Description: "A default Favicon",
			KDexOpenAPIInternal: kdexv1alpha1.KDexOpenAPIInternal{
				Get: &openapi.Operation{
					Responses: openapi.NewResponses(
						openapi.WithName("200", &openapi.Response{
							Content: openapi.NewContentWithSchema(
								nil,
								[]string{"image/svg+xml"},
							),
						}),
					),
				},
			},
			Path:    path,
			Summary: "Request Sniffer Documentation",
		},
		Type: InternalPathType,
	}
}

func (th *HostHandler) mergeOperations(dest, src *kdexv1alpha1.KDexOpenAPI) {
	if src.Connect != nil {
		dest.Connect = src.Connect
	}
	if src.Delete != nil {
		dest.Delete = src.Delete
	}
	if src.Get != nil {
		dest.Get = src.Get
	}
	if src.Head != nil {
		dest.Head = src.Head
	}
	if src.Options != nil {
		dest.Options = src.Options
	}
	if src.Patch != nil {
		dest.Patch = src.Patch
	}
	if src.Post != nil {
		dest.Post = src.Post
	}
	if src.Put != nil {
		dest.Put = src.Put
	}
	if src.Trace != nil {
		dest.Trace = src.Trace
	}
}

func (th *HostHandler) messagePrinter(translations *Translations, tag language.Tag) *message.Printer {
	return message.NewPrinter(
		tag,
		message.Catalog(translations.Catalog()),
	)
}

func (th *HostHandler) muxWithDefaultsLocked(registeredPaths map[string]PathInfo) *http.ServeMux {
	mux := http.NewServeMux()

	th.faviconHandler(mux, registeredPaths)
	th.navigationHandler(mux, registeredPaths)
	th.translationHandler(mux, registeredPaths)
	th.snifferHandler(mux, registeredPaths)
	th.openapiHandler(mux, registeredPaths)

	// TODO: implement a state handler
	// TODO: implement an oauth handler
	// TODO: implement a check handler

	th.unimplementedHandler("GET /~/check/", mux, registeredPaths)
	th.unimplementedHandler("GET /~/oauth/", mux, registeredPaths)
	th.unimplementedHandler("GET /~/state/", mux, registeredPaths)

	return mux
}

func (th *HostHandler) navigationHandler(mux *http.ServeMux, registeredPaths map[string]PathInfo) {
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

	th.registerPath(path, PathInfo{
		API: kdexv1alpha1.KDexOpenAPI{
			Description: "Provides dynamic HTML fragments for page navigation components, supporting localization and breadcrumb contexts.",
			KDexOpenAPIInternal: kdexv1alpha1.KDexOpenAPIInternal{
				Get: &openapi.Operation{
					Parameters: th.extractParameters(path, ""),
					Responses: openapi.NewResponses(
						openapi.WithName("200", &openapi.Response{
							Description: openapi.Ptr("HTML navigation fragment"),
							Content: openapi.NewContentWithSchema(
								nil,
								[]string{"text/html"},
							),
						}),
						openapi.WithName("400", &openapi.Response{
							Description: openapi.Ptr("Unable to acertain the locale from {l10n} parameter"),
						}),
						openapi.WithName("404", &openapi.Response{
							Description: openapi.Ptr("Resource not found"),
						}),
						openapi.WithName("500", &openapi.Response{
							Description: openapi.Ptr("Internal server error"),
						}),
					),
				},
			},
			Path:    path,
			Summary: "Dynamic Navigation Fragment Provider",
		},
		Type: InternalPathType,
	}, registeredPaths)
}

func (th *HostHandler) openapiHandler(mux *http.ServeMux, registeredPaths map[string]PathInfo) {
	const path = "/~/openapi/"

	// Register the path itself so it appears in the spec
	th.registerPath(path, PathInfo{
		API: kdexv1alpha1.KDexOpenAPI{
			Description: "Serves the generated OpenAPI 3.0 specification for this host.",
			KDexOpenAPIInternal: kdexv1alpha1.KDexOpenAPIInternal{
				Get: &openapi.Operation{
					Responses: openapi.NewResponses(
						openapi.WithName("200", &openapi.Response{
							Description: openapi.Ptr("OpenAPI documentation"),
							Content: openapi.NewContentWithSchema(
								openapi.NewSchema().WithAnyAdditionalProperties(),
								[]string{"application/json"},
							),
						}),
						openapi.WithName("500", &openapi.Response{
							Description: openapi.Ptr("Failed to marshal OpenAPI spec"),
						}),
					),
				},
			},
			Path:    path,
			Summary: "OpenAPI Specification",
		},
		Type: InternalPathType,
	}, registeredPaths)

	mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
		th.mu.RLock()
		defer th.mu.RUnlock()

		spec := th.buildOpenAPI(th.registeredPaths)

		jsonBytes, err := json.Marshal(spec)
		if err != nil {
			http.Error(w, "Failed to marshal OpenAPI spec", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(jsonBytes)
	})
}

func (th *HostHandler) registerPath(path string, info PathInfo, m map[string]PathInfo) {
	current, ok := m[path]
	if !ok {
		if info.API.Path == "" {
			info.API.Path = path
		}
		m[path] = info
		return
	}

	th.mergeOperations(&current.API, &info.API)

	if current.API.Summary == "" {
		current.API.Summary = info.API.Summary
	}
	if current.API.Description == "" {
		current.API.Description = info.API.Description
	}
	if current.API.Path == "" {
		current.API.Path = path
	}

	m[path] = current
}

func (th *HostHandler) registerPathFromPattern(pattern string, registeredPaths map[string]PathInfo, pathType PathType) {
	parts := strings.Split(pattern, " ")
	method := "GET"
	path := pattern
	if len(parts) > 1 {
		method = parts[0]
		path = parts[1]
	}

	info := PathInfo{
		API: kdexv1alpha1.KDexOpenAPI{
			Description: fmt.Sprintf("Internal system endpoint providing %s functionality. NOT YET IMPLEMENTED!", path),
			Path:        path,
			Summary:     fmt.Sprintf("System Endpoint: %s", path),
		},
		Type: pathType,
	}
	th.setOperation(&info.API, method, nil)

	th.registerPath(path, info, registeredPaths)
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

func (th *HostHandler) setOperation(api *kdexv1alpha1.KDexOpenAPI, method string, op *openapi.Operation) {
	if op == nil {
		op = &openapi.Operation{}
	}

	// Extract and set parameters from the path if not already set
	if op.Parameters == nil && api.Path != "" {
		op.Parameters = th.extractParameters(api.Path, "")
	}

	switch strings.ToUpper(method) {
	case "CONNECT":
		api.Connect = op
	case "DELETE":
		api.Delete = op
	case "GET":
		api.Get = op
	case "HEAD":
		api.Head = op
	case "OPTIONS":
		api.Options = op
	case "PATCH":
		api.Patch = op
	case "POST":
		api.Post = op
	case "PUT":
		api.Put = op
	case "TRACE":
		api.Trace = op
	}
}

func (th *HostHandler) snifferHandler(mux *http.ServeMux, registeredPaths map[string]PathInfo) {
	if th.Sniffer != nil {
		const path = "/~/sniffer/docs"
		mux.HandleFunc("GET "+path, th.Sniffer.DocsHandler)
		registeredPaths[path] = PathInfo{
			API: kdexv1alpha1.KDexOpenAPI{
				Description: "Provides Markdown documentation for the Request Sniffer's supported headers and behaviors.",
				KDexOpenAPIInternal: kdexv1alpha1.KDexOpenAPIInternal{
					Get: &openapi.Operation{
						Parameters: th.extractParameters(path, ""),
						Responses:  openapi.NewResponses(),
					},
				},
				Path:    path,
				Summary: "Request Sniffer Documentation",
			},
			Type: InternalPathType,
		}
	}
}

func (th *HostHandler) translationHandler(mux *http.ServeMux, registeredPaths map[string]PathInfo) {
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
		Parameters: th.extractParameters(path, "key=one&key=two"),
		Responses: openapi.NewResponses(
			openapi.WithName("200", &openapi.Response{
				Description: openapi.Ptr("JSON translation map"),
				Content: openapi.NewContentWithSchema(
					openapi.NewSchema().WithAnyAdditionalProperties(),
					[]string{"application/json"},
				),
			}),
			openapi.WithName("500", &openapi.Response{
				Description: openapi.Ptr("Internal server error"),
			}),
		),
	}

	th.registerPath(path, PathInfo{
		API: kdexv1alpha1.KDexOpenAPI{
			Description: "Provides a JSON map of localization keys and their translated values for a given language tag.",
			KDexOpenAPIInternal: kdexv1alpha1.KDexOpenAPIInternal{
				Get: op,
			},
			Path:    path,
			Summary: "Localization Key Provider",
		},
		Type: InternalPathType,
	}, registeredPaths)
}

func (th *HostHandler) unimplementedHandler(pattern string, mux *http.ServeMux, registeredPaths map[string]PathInfo) {
	mux.HandleFunc(
		pattern,
		func(w http.ResponseWriter, r *http.Request) {
			th.log.V(1).Info("unimplemented handler", "path", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_, err := fmt.Fprintf(w, `{"path": "%s", "message": "Nothing here yet..."}`, r.URL.Path)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		},
	)

	th.registerPathFromPattern(pattern, registeredPaths, InternalPathType)
}
