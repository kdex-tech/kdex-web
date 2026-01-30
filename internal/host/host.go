package host

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	openapi "github.com/getkin/kin-openapi/openapi3"
	"github.com/go-logr/logr"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/crds/render"
	"kdex.dev/web/internal/auth"
	"kdex.dev/web/internal/host/ico"
	kdexhttp "kdex.dev/web/internal/http"
	ko "kdex.dev/web/internal/openapi"
	"kdex.dev/web/internal/page"
	"kdex.dev/web/internal/sniffer"
	"kdex.dev/web/internal/utils"
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

func (hh *HostHandler) AddOrUpdateTranslation(name string, translation *kdexv1alpha1.KDexTranslationSpec) {
	if translation == nil {
		return
	}
	hh.log.V(1).Info("add or update translation", "translation", name)
	hh.mu.Lock()
	hh.translationResources[name] = *translation
	hh.mu.Unlock()
	hh.RebuildMux() // Called after lock is released
}

func (hh *HostHandler) AddOrUpdateUtilityPage(ph page.PageHandler) {
	if ph.UtilityPage == nil {
		return
	}
	hh.log.V(1).Info("add or update utility page", "name", ph.Name, "type", ph.UtilityPage.Type)
	hh.mu.Lock()
	hh.utilityPages[ph.UtilityPage.Type] = ph
	hh.mu.Unlock()
	hh.RebuildMux()
}

func (hh *HostHandler) FootScriptToHTML(handler page.PageHandler) string {
	var buffer bytes.Buffer
	separator := ""

	for _, script := range hh.scripts {
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

func (hh *HostHandler) GetUtilityPageHandler(name kdexv1alpha1.KDexUtilityPageType) page.PageHandler {
	hh.mu.RLock()
	defer hh.mu.RUnlock()
	ph, ok := hh.utilityPages[name]
	if !ok {
		return page.PageHandler{}
	}
	return ph
}

func (hh *HostHandler) HeadScriptToHTML(handler page.PageHandler) string {
	packageReferences := []kdexv1alpha1.PackageReference{}
	packageReferences = append(packageReferences, hh.packageReferences...)
	packageReferences = append(packageReferences, handler.PackageReferences...)

	var buffer bytes.Buffer
	separator := ""

	if len(packageReferences) > 0 {
		buffer.WriteString("<script type=\"importmap\">\n")
		buffer.WriteString(hh.importmap)
		buffer.WriteString("\n</script>\n")

		buffer.WriteString("<script type=\"module\">\n")
		for _, pr := range packageReferences {
			buffer.WriteString(separator)
			buffer.WriteString(pr.ToImportStatement())
			separator = "\n"
		}
		buffer.WriteString("\n</script>")
	}

	for _, script := range hh.scripts {
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

func (hh *HostHandler) L10nRender(
	handler page.PageHandler,
	pageMap map[string]any,
	l language.Tag,
	extraTemplateData map[string]any,
	translations *Translations,
) (string, error) {

	// make sure everything passed to the renderer is mutation safe (i.e. copy it)

	renderer := render.Renderer{
		BasePath:        handler.BasePath(),
		BrandName:       hh.host.BrandName,
		Contents:        handler.ContentToHTMLMap(),
		DefaultLanguage: hh.defaultLanguage,
		Extra:           maps.Clone(extraTemplateData),
		Footer:          handler.Footer,
		FootScript:      hh.FootScriptToHTML(handler),
		Header:          handler.Header,
		HeadScript:      hh.HeadScriptToHTML(handler),
		Language:        l.String(),
		Languages:       hh.availableLanguages(translations),
		LastModified:    time.Now(),
		MessagePrinter:  hh.messagePrinter(translations, l),
		Meta:            hh.MetaToString(handler, l),
		Navigations:     handler.NavigationToHTMLMap(),
		Organization:    hh.host.Organization,
		PageMap:         maps.Clone(pageMap),
		PatternPath:     handler.PatternPath(),
		TemplateContent: handler.MainTemplate,
		TemplateName:    handler.Name,
		Theme:           hh.ThemeAssetsToString(),
		Title:           handler.Label(),
	}

	return renderer.RenderPage()
}

func (hh *HostHandler) L10nRenders(
	handler page.PageHandler,
	pageMaps map[language.Tag]map[string]any,
	translations *Translations,
) map[string]string {
	l10nRenders := make(map[string]string)
	for _, l := range translations.Languages() {
		rendered, err := hh.L10nRender(handler, pageMaps[l], l, map[string]any{}, translations)
		if err != nil {
			hh.log.Error(err, "failed to render page for language", "page", handler.Name, "language", l)
			continue
		}
		l10nRenders[l.String()] = rendered
	}
	return l10nRenders
}

func (hh *HostHandler) MetaToString(handler page.PageHandler, l language.Tag) string {
	var buffer bytes.Buffer

	if len(hh.host.Assets) > 0 {
		buffer.WriteString(hh.host.Assets.String())
		buffer.WriteRune('\n')
	}

	basePath := handler.BasePath()
	if l.String() != hh.defaultLanguage {
		basePath = "/" + l.String() + basePath
	}
	patternPath := handler.PatternPath()
	if l.String() != hh.defaultLanguage {
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
	// data-path-separator="/_/"
	// data-state-endpoint="/~/state"

	return buffer.String()
}

func (hh *HostHandler) RebuildMux() {
	hh.log.V(1).Info("rebuilding mux")
	hh.mu.RLock()

	if hh.host == nil {
		hh.mu.RUnlock()
		return
	}

	// copy fields that we need while under RLock
	defaultLanguageResource := hh.defaultLanguage
	translationResources := maps.Clone(hh.translationResources)

	newTranslations, err := NewTranslations(defaultLanguageResource, translationResources)
	if err != nil {
		hh.log.Error(err, "failed to rebuild translations")
		hh.mu.RUnlock()
		return
	}

	registeredPaths := map[string]ko.PathInfo{}
	maps.Copy(registeredPaths, hh.pathsCollectedInReconcile)

	mux := hh.muxWithDefaultsLocked(registeredPaths)

	pageHandlers := hh.Pages.List()

	if len(pageHandlers) == 0 {
		handler := func(w http.ResponseWriter, r *http.Request) {
			l, err := kdexhttp.GetLang(r, hh.defaultLanguage, hh.Translations.Languages())
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			rendered := hh.renderUtilityPage(
				kdexv1alpha1.AnnouncementUtilityPageType,
				l,
				map[string]any{},
				&hh.Translations,
			)

			if rendered == "" {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}

			hh.log.V(1).Info("serving announcement page", "language", l.String())

			w.Header().Set("Content-Language", l.String())
			w.Header().Set("Content-Type", "text/html")

			_, err = w.Write([]byte(rendered))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		}

		mux.HandleFunc("GET /{$}", handler)
		mux.HandleFunc("GET /{l10n}/{$}", handler)

		hh.mu.RUnlock()
		hh.mu.Lock()
		hh.Translations = *newTranslations
		hh.registeredPaths = registeredPaths
		hh.Mux = mux
		hh.mu.Unlock()

		return
	}

	renderedPages := []pageRender{}

	for _, ph := range pageHandlers {
		basePath := ph.BasePath()

		if basePath == "" {
			hh.log.V(1).Info("somehow page has empty basePath, skipping", "page", ph.Name)
			continue
		}

		l10nRenders := hh.L10nRenders(ph, nil, newTranslations)
		renderedPages = append(renderedPages, pageRender{ph: ph, l10nRenders: l10nRenders})
	}

	hh.mu.RUnlock()

	for _, pr := range renderedPages {
		hh.addHandlerAndRegister(mux, pr, registeredPaths)
	}

	hh.mu.Lock()
	hh.Translations = *newTranslations
	hh.registeredPaths = registeredPaths
	hh.Mux = mux
	hh.mu.Unlock()
}

func (hh *HostHandler) RemoveTranslation(name string) {
	hh.log.V(1).Info("delete translation", "translation", name)
	hh.mu.Lock()
	delete(hh.translationResources, name)
	hh.mu.Unlock()

	hh.RebuildMux() // Called after lock is released
}

func (hh *HostHandler) RemoveUtilityPage(name string) {
	hh.log.V(1).Info("delete utility page", "name", name)
	hh.mu.Lock()
	for t, ph := range hh.utilityPages {
		if ph.Name == name {
			delete(hh.utilityPages, t)
			break
		}
	}
	hh.mu.Unlock()

	hh.RebuildMux() // Called after lock is released
}

func (hh *HostHandler) SecuritySchemes() *openapi.SecuritySchemes {
	req := &openapi.SecuritySchemes{}
	bearerScheme := openapi.NewJWTSecurityScheme()
	bearerScheme.Description = "Bearer Token - This is the default scheme"
	// For now we assume that if a login page is specified we want to default to bearer auth
	// as the preferred mode of authentication for auto-generated functions.
	if hh.host != nil && hh.host.UtilityPages != nil && hh.host.UtilityPages.LoginRef != nil {
		(*req)["bearer"] = &openapi.SecuritySchemeRef{
			Value: bearerScheme,
		}
	} else if hh.host != nil {
		(*req)["bearer"] = &openapi.SecuritySchemeRef{
			Value: bearerScheme,
		}
	}

	return req
}

func (hh *HostHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hh.mu.RLock()
	mux := hh.Mux
	hh.mu.RUnlock()

	if mux == nil {
		hh.serveError(w, r, http.StatusNotFound, "not found")
		return
	}

	wrappedMux := hh.authConfig.AddAuthentication(mux)
	wrappedMux = hh.DesignMiddleware(wrappedMux)
	wrappedMux.ServeHTTP(w, r)
}

func (hh *HostHandler) SetHost(
	ctx context.Context,
	host *kdexv1alpha1.KDexHostSpec,
	packageReferences []kdexv1alpha1.PackageReference,
	themeAssets []kdexv1alpha1.Asset,
	scripts []kdexv1alpha1.ScriptDef,
	importmap string,
	paths map[string]ko.PathInfo,
	functions []kdexv1alpha1.KDexFunction,
	authExchanger *auth.Exchanger,
	authConfig *auth.Config,
) {
	hh.mu.Lock()
	hh.host = host
	hh.defaultLanguage = host.DefaultLang
	hh.favicon = ico.NewICO(host.FaviconSVGTemplate, render.TemplateData{
		BrandName:       host.BrandName,
		DefaultLanguage: host.DefaultLang,
		Organization:    host.Organization,
	})
	hh.openapiBuilder = ko.Builder{
		SecuritySchemes: hh.SecuritySchemes(),
		TypesToInclude: utils.MapSlice(host.OpenAPI.TypesToInclude, func(i kdexv1alpha1.TypeToInclude) ko.PathType {
			switch i {
			case kdexv1alpha1.TypeBACKEND:
				return ko.BackendPathType
			case kdexv1alpha1.TypeFUNCTION:
				return ko.FunctionPathType
			case kdexv1alpha1.TypePAGE:
				return ko.PagePathType
			default:
				return ko.SystemPathType
			}
		}),
	}
	hh.packageReferences = packageReferences
	hh.pathsCollectedInReconcile = paths
	hh.themeAssets = themeAssets
	hh.scripts = scripts

	var snif *sniffer.RequestSniffer
	if host.DevMode {
		snif = &sniffer.RequestSniffer{
			BasePathRegex:   (&kdexv1alpha1.API{}).BasePathRegex(),
			Client:          hh.client,
			Functions:       functions,
			HostName:        hh.Name,
			ItemPathRegex:   (&kdexv1alpha1.API{}).ItemPathRegex(),
			OpenAPIBuilder:  hh.openapiBuilder,
			Namespace:       hh.Namespace,
			SecuritySchemes: hh.SecuritySchemes(),
		}
	}

	hh.sniffer = snif
	hh.importmap = importmap

	if authConfig != nil {
		hh.authConfig = authConfig
		hh.authChecker = auth.NewAuthorizationChecker(authConfig.AnonymousGrants, hh.log.WithName("authChecker"))
		hh.authExchanger = authExchanger
	}

	hh.mu.Unlock()
	hh.RebuildMux()
}

func (hh *HostHandler) ThemeAssetsToString() string {
	var buffer bytes.Buffer

	for _, asset := range hh.themeAssets {
		buffer.WriteString(asset.ToTag())
		buffer.WriteRune('\n')
	}

	return buffer.String()
}

func (hh *HostHandler) availableLanguages(translations *Translations) []string {
	availableLangs := []string{}

	for _, tag := range translations.Languages() {
		availableLangs = append(availableLangs, tag.String())
	}

	return availableLangs
}

func (hh *HostHandler) isSecure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

func (hh *HostHandler) messagePrinter(translations *Translations, tag language.Tag) *message.Printer {
	return message.NewPrinter(
		tag,
		message.Catalog(translations.Catalog()),
	)
}

func (hh *HostHandler) muxWithDefaultsLocked(registeredPaths map[string]ko.PathInfo) *http.ServeMux {
	mux := http.NewServeMux()

	hh.faviconHandler(mux, registeredPaths)
	hh.navigationHandler(mux, registeredPaths)
	hh.translationHandler(mux, registeredPaths)
	hh.loginHandler(mux, registeredPaths)
	hh.jwksHandler(mux, registeredPaths)
	hh.oauthHandler(mux, registeredPaths)
	hh.stateHandler(mux, registeredPaths)
	hh.snifferHandler(mux, registeredPaths)
	hh.openapiHandler(mux, registeredPaths)
	hh.schemaHandler(mux, registeredPaths)

	// TODO: implement a check handler

	// hh.unimplementedHandler("GET /~/check/", mux, registeredPaths)

	return mux
}

func (hh *HostHandler) pageRequirements(ph *page.PageHandler) []kdexv1alpha1.SecurityRequirement {
	hh.mu.RLock()
	defer hh.mu.RUnlock()
	var requirements []kdexv1alpha1.SecurityRequirement
	if hh.host.Security != nil {
		requirements = *hh.host.Security
	}
	if ph.Page.Security != nil {
		requirements = *ph.Page.Security
	}
	return requirements
}

func (hh *HostHandler) registerPath(path string, info ko.PathInfo, m map[string]ko.PathInfo) {
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

func (hh *HostHandler) renderUtilityPage(utilityType kdexv1alpha1.KDexUtilityPageType, l language.Tag, extraTemplateData map[string]any, translations *Translations) string {
	ph, ok := hh.utilityPages[utilityType]
	if !ok {
		return ""
	}

	rendered, err := hh.L10nRender(ph, map[string]any{}, l, extraTemplateData, translations)
	if err != nil {
		hh.log.Error(err, "failed to render utility page", "page", ph.Name, "language", l)
		return ""
	}

	return rendered
}

func (hh *HostHandler) serverAddress(r *http.Request) string {
	scheme := "http"
	if hh.isSecure(r) {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}

func (hh *HostHandler) serveError(w http.ResponseWriter, r *http.Request, code int, msg string) {
	hh.mu.RLock()
	l, err := kdexhttp.GetLang(r, hh.defaultLanguage, hh.Translations.Languages())
	if err != nil {
		l = language.Make(hh.defaultLanguage)
	}

	// collect stacktrace
	stacktrace := string(debug.Stack())

	hh.log.V(2).Info("generating error page", "requestURI", r.URL.Path, "code", code, "msg", msg, "language", l, "stacktrace", stacktrace)

	rendered := hh.renderUtilityPage(
		kdexv1alpha1.ErrorUtilityPageType,
		l,
		map[string]any{"ErrorCode": code, "ErrorCodeString": http.StatusText(code), "ErrorMessage": msg},
		&hh.Translations,
	)
	hh.mu.RUnlock()

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

func toFinalPath(path string) string {
	if !strings.HasSuffix(path, "/") {
		path = path + "/"
	}
	path = path + "{$}"
	return path
}
