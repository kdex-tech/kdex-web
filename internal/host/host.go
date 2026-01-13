package host

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/message/catalog"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/crds/render"
	kdexhttp "kdex.dev/web/internal/http"
	"kdex.dev/web/internal/page"
)

type HostHandler struct {
	Mux          *http.ServeMux
	Name         string
	Namespace    string
	Pages        *page.PageStore
	Translations Translations

	defaultLanguage      string
	host                 *kdexv1alpha1.KDexHostSpec
	importmap            string
	log                  logr.Logger
	mu                   sync.RWMutex
	packageReferences    []kdexv1alpha1.PackageReference
	registeredPaths      map[string]PathInfo
	scripts              []kdexv1alpha1.ScriptDef
	themeAssets          []kdexv1alpha1.Asset
	translationResources map[string]kdexv1alpha1.KDexTranslationSpec
	utilityPages         map[kdexv1alpha1.KDexUtilityPageType]page.PageHandler

	// TODO: register all routes added to the mux so that we can map them in openapi
}

type Translations struct {
	catalog *catalog.Builder
	keys    []string
}

func NewTranslations(defaultLanguage string, translations map[string]kdexv1alpha1.KDexTranslationSpec) (*Translations, error) {
	catalogBuilder := catalog.NewBuilder()

	if err := catalogBuilder.SetString(language.Make(defaultLanguage), "_", "_"); err != nil {
		return nil, fmt.Errorf("failed to set default translation %s %s", defaultLanguage, "_")
	}

	keys := []string{}
	for name, translation := range translations {
		for _, tr := range translation.Translations {
			for key, value := range tr.KeysAndValues {
				if err := catalogBuilder.SetString(language.Make(tr.Lang), key, value); err != nil {
					return nil, fmt.Errorf("failed to set translation %s %s %s %s", name, tr.Lang, key, value)
				}
				keys = append(keys, key)
			}
		}
	}

	return &Translations{
		catalog: catalogBuilder,
		keys:    keys,
	}, nil
}

func (t *Translations) Catalog() *catalog.Builder {
	return t.catalog
}

func (t *Translations) Keys() []string {
	return t.keys
}

func (t *Translations) Languages() []language.Tag {
	return t.catalog.Languages()
}

type errorResponseWriter struct {
	http.ResponseWriter
	statusCode  int
	statusMsg   string
	wroteHeader bool
}

const (
	kdexUIMetaTemplate = `<meta
  name="kdex-ui"
  data-page-basepath="%s"
  data-navigation-endpoint="/~/navigation/{name}/{l10n}/{basePathMinusLeadingSlash...}"
  data-page-patternpath="%s"
/>
`
)

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

func (th *HostHandler) DeregisterPath(path string) {
	delete(th.registeredPaths, path)
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

func NewHostHandler(name string, namespace string, log logr.Logger) *HostHandler {
	th := &HostHandler{
		Name:                 name,
		Namespace:            namespace,
		defaultLanguage:      "en",
		log:                  log.WithValues("host", name),
		translationResources: map[string]kdexv1alpha1.KDexTranslationSpec{},
		utilityPages:         map[kdexv1alpha1.KDexUtilityPageType]page.PageHandler{},
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

	mux := th.muxWithDefaultsLocked()

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

		patternPath := ph.Page.PatternPath
		if patternPath != "" {
			mux.HandleFunc("GET "+patternPath, handler)
			mux.HandleFunc("GET /{l10n}"+patternPath, handler)
		}
	}

	th.mu.Lock()
	th.Translations = *newTranslations
	th.Mux = mux
	th.mu.Unlock()
}

func (th *HostHandler) RegisterPath(path string, pathInfo PathInfo) {
	th.registeredPaths[path] = pathInfo
}

func (th *HostHandler) RegisteredPaths() map[string]PathInfo {
	th.mu.RLock()
	defer th.mu.RUnlock()
	out := make(map[string]PathInfo, len(th.registeredPaths))
	for p, i := range th.registeredPaths {
		out[p] = i
	}
	return out
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
		th.serveError(w, r, ew.statusCode, ew.statusMsg)
	}
}

func (th *HostHandler) SetHost(
	host *kdexv1alpha1.KDexHostSpec,
	packageReferences []kdexv1alpha1.PackageReference,
	themeAssets []kdexv1alpha1.Asset,
	scripts []kdexv1alpha1.ScriptDef,
	importmap string,
) {
	th.mu.Lock()
	th.defaultLanguage = host.DefaultLang
	th.host = host
	th.packageReferences = packageReferences
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

func (ew *errorResponseWriter) Write(b []byte) (int, error) {
	if ew.statusCode >= 400 {
		// Drop original error body, we will render our own
		ew.statusMsg = string(b)
		return len(b), nil
	}
	if !ew.wroteHeader {
		ew.WriteHeader(http.StatusOK)
	}
	return ew.ResponseWriter.Write(b)
}

func (ew *errorResponseWriter) WriteHeader(code int) {
	if ew.wroteHeader {
		return
	}
	ew.statusCode = code
	if code >= 400 {
		return
	}
	ew.wroteHeader = true
	ew.ResponseWriter.WriteHeader(code)
}

func (th *HostHandler) availableLanguages(translations *Translations) []string {
	availableLangs := []string{}

	for _, tag := range translations.Languages() {
		availableLangs = append(availableLangs, tag.String())
	}

	return availableLangs
}

func (th *HostHandler) unimplementedHandler(path string, mux *http.ServeMux) {
	mux.HandleFunc(
		path,
		func(w http.ResponseWriter, r *http.Request) {
			th.log.V(1).Info("unimplemented handler", "path", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_, err := fmt.Fprintf(w, `{"path": "%s", "message": "Nothing here yet..."}`, r.URL.Path)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		},
	)
}

func (th *HostHandler) messagePrinter(translations *Translations, tag language.Tag) *message.Printer {
	return message.NewPrinter(
		tag,
		message.Catalog(translations.Catalog()),
	)
}

func (th *HostHandler) muxWithDefaultsLocked() *http.ServeMux {
	mux := http.NewServeMux()

	th.navigationHandler(mux)
	th.translationHandler(mux)

	// TODO: implement a state handler
	// TODO: implement an oauth handler
	// TODO: implement a check handler
	// TODO: implement an openapi handler

	th.unimplementedHandler("GET /~/check/", mux)
	th.unimplementedHandler("GET /~/oauth/", mux)
	th.unimplementedHandler("GET /~/state/", mux)
	th.unimplementedHandler("GET /~/openapi/", mux)

	return mux
}

func (th *HostHandler) navigationHandler(mux *http.ServeMux) {
	mux.HandleFunc(
		"GET /~/navigation/{navKey}/{l10n}/{basePathMinusLeadingSlash...}",
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
}

func (th *HostHandler) translationHandler(mux *http.ServeMux) {
	mux.HandleFunc(
		"GET /~/translation/{l10n}",
		func(w http.ResponseWriter, r *http.Request) {
			l, err := kdexhttp.GetLang(r, th.defaultLanguage, th.Translations.Languages())
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			// Get all the keys and values for the given language
			keys := th.Translations.Keys()
			// check query parameters for array of keys
			queryParams := r.URL.Query()
			keysParam := queryParams["keys"]
			if len(keysParam) > 0 {
				keys = keysParam
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

	th.log.V(2).Info("generating error page", "code", code, "msg", msg, "language", l, "stacktrace", stacktrace)

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
