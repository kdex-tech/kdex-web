package host

import (
	"bytes"
	"fmt"
	"net/http"
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
	Name                 string
	Mux                  *http.ServeMux
	Pages                *page.PageStore
	ScriptLibraries      []kdexv1alpha1.KDexScriptLibrarySpec
	Translations         *catalog.Builder
	assets               []kdexv1alpha1.Asset
	defaultLanguage      string
	host                 *kdexv1alpha1.KDexHostSpec
	importmap            string
	log                  logr.Logger
	mu                   sync.RWMutex
	translationResources map[string]kdexv1alpha1.KDexTranslationSpec
}

func NewHostHandler(name string, log logr.Logger) *HostHandler {
	th := &HostHandler{
		Name:                 name,
		defaultLanguage:      "en",
		log:                  log.WithValues("host", name),
		translationResources: map[string]kdexv1alpha1.KDexTranslationSpec{},
	}

	catalogBuilder := catalog.NewBuilder()
	if err := catalogBuilder.SetString(language.Make(th.defaultLanguage), "_", "_"); err != nil {
		th.log.Error(err, "failed to add default placeholder translation")
	}

	th.Translations = catalogBuilder
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

	for _, scriptLibrary := range th.ScriptLibraries {
		for _, script := range scriptLibrary.Scripts {
			buffer.WriteString(separator)
			buffer.WriteString(script.ToFootTag())
			separator = "\n"
		}
	}
	for _, script := range handler.Scripts {
		buffer.WriteString(separator)
		buffer.WriteString(script.ToFootTag())
		separator = "\n"
	}

	return buffer.String()
}

func (th *HostHandler) HeadScriptToHTML(handler page.PageHandler) string {
	packageReferences := []kdexv1alpha1.PackageReference{}
	for _, scriptLibrary := range th.ScriptLibraries {
		if scriptLibrary.PackageReference != nil {
			packageReferences = append(packageReferences, *scriptLibrary.PackageReference)
		}
	}
	packageReferences = append(packageReferences, handler.PackageReferences...)

	var buffer bytes.Buffer
	separator := ""

	if len(packageReferences) > 0 {
		buffer.WriteString("<script type=\"importmap\">\n")
		buffer.WriteString(th.importmap)
		buffer.WriteString("</script>\n")

		buffer.WriteString("<script type=\"module\">\n")
		for _, pr := range packageReferences {
			buffer.WriteString(separator)
			buffer.WriteString(pr.ToImportStatement())
			separator = "\n"
		}
		buffer.WriteString("</script>")
	}

	for _, scriptLibrary := range th.ScriptLibraries {
		for _, script := range scriptLibrary.Scripts {
			buffer.WriteString(separator)
			buffer.WriteString(script.ToHeadTag())
			separator = "\n"
		}
	}
	for _, script := range handler.Scripts {
		buffer.WriteString(separator)
		buffer.WriteString(script.ToHeadTag())
		separator = "\n"
	}

	return buffer.String()
}

func (th *HostHandler) L10nRenderLocked(
	handler page.PageHandler,
	pageMap map[string]interface{},
	l language.Tag,
) (string, error) {
	renderer := render.Renderer{
		BasePath:        handler.Page.BasePath,
		BrandName:       th.host.BrandName,
		Contents:        handler.ContentToHTMLMap(),
		DefaultLanguage: th.defaultLanguage,
		Footer:          handler.Footer,
		FootScript:      th.FootScriptToHTML(handler),
		Header:          handler.Header,
		HeadScript:      th.HeadScriptToHTML(handler),
		Language:        l.String(),
		Languages:       th.availableLanguagesLocked(),
		LastModified:    time.Now(),
		MessagePrinter:  th.messagePrinterLocked(l),
		Meta:            th.MetaToString(handler),
		Navigations:     handler.NavigationToHTMLMap(),
		Organization:    th.host.Organization,
		PageMap:         pageMap,
		PatternPath:     handler.Page.PatternPath,
		TemplateContent: handler.MainTemplate,
		TemplateName:    handler.Name,
		Theme:           th.host.Assets.String(),
		Title:           handler.Page.Label,
	}

	return renderer.RenderPage()
}

func (th *HostHandler) L10nRendersLocked(
	handler page.PageHandler,
	pageMaps map[language.Tag]map[string]interface{},
) map[string]string {
	l10nRenders := make(map[string]string)
	for _, l := range th.Translations.Languages() {
		rendered, err := th.L10nRenderLocked(handler, pageMaps[l], l)
		if err != nil {
			th.log.Error(err, "failed to render page for language", "page", handler.Name, "language", l)
			continue
		}
		l10nRenders[l.String()] = rendered
	}
	return l10nRenders
}

const (
	announcementPageTemplate = `<!DOCTYPE html>
<html lang="{{.Language}}">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	{{.Meta}}
	<title>{{l10n "announcement.title" .BrandName}}</title>
	{{.HeadScript}}
	{{.Theme}}
</head>
<body>
	<main style="display: flex; flex-direction: column; align-items: center; justify-content: center; min-height: 90vh; padding: 2rem; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;">
		<div style="max-width: 600px; text-align: center;">
			<h1 style="font-size: 2.5rem; margin-bottom: 1rem; color: #333;">{{l10n "announcement.title" .BrandName}}</h1>
			<p style="font-size: 1.125rem; line-height: 1.6; color: #666; margin-bottom: 2rem;">{{l10n "announcement.message"}}</p>
			<div style="padding: 1rem; background-color: #f5f5f5; border-radius: 8px; margin-top: 2rem;">
				<p style="font-size: 0.875rem; color: #888; margin: 0;">{{l10n "announcement.organization" .Organization}}</p>
			</div>
		</div>
	</main>
	{{.FootScript}}
</body>
</html>`
)

func (th *HostHandler) renderAnnouncementPageLocked() map[string]string {
	l10nRenders := make(map[string]string)

	meta := ""
	if len(th.host.Assets) > 0 {
		meta = th.host.Assets.String()
	}

	for _, l := range th.Translations.Languages() {
		renderer := render.Renderer{
			BasePath:        "/",
			BrandName:       th.host.BrandName,
			Contents:        map[string]string{},
			DefaultLanguage: th.defaultLanguage,
			Footer:          "",
			FootScript:      th.FootScriptToHTML(page.PageHandler{}),
			Header:          "",
			HeadScript:      th.HeadScriptToHTML(page.PageHandler{}),
			Language:        l.String(),
			Languages:       th.availableLanguagesLocked(),
			LastModified:    time.Now(),
			MessagePrinter:  th.messagePrinterLocked(l),
			Meta:            meta,
			Navigations:     map[string]string{},
			Organization:    th.host.Organization,
			PageMap:         map[string]interface{}{},
			PatternPath:     "",
			TemplateContent: announcementPageTemplate,
			TemplateName:    "announcement",
			Theme:           th.host.Assets.String(),
			Title:           "",
		}

		rendered, err := renderer.RenderPage()
		if err != nil {
			th.log.Error(err, "failed to render announcement page for language", "language", l)
			continue
		}
		l10nRenders[l.String()] = rendered
	}

	return l10nRenders
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

func (th *HostHandler) MetaToString(handler page.PageHandler) string {
	var buffer bytes.Buffer

	if len(th.host.Assets) > 0 {
		buffer.WriteString(th.host.Assets.String())
		buffer.WriteRune('\n')
	}

	fmt.Fprintf(
		&buffer,
		kdexUIMetaTemplate,
		handler.Page.BasePath,
		handler.Page.PatternPath,
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
	th.mu.Lock()
	defer th.mu.Unlock()

	if th.host == nil {
		return
	}

	th.rebuildTranslationsLocked()
	mux := th.muxWithDefaultsLocked()

	pageHandlers := th.Pages.List()

	if len(pageHandlers) == 0 {
		// Render announcement page for all languages
		l10nRenders := th.renderAnnouncementPageLocked()

		handler := func(w http.ResponseWriter, r *http.Request) {
			l := kdexhttp.GetLang(r, th.defaultLanguage, th.Translations.Languages())

			rendered, ok := l10nRenders[l.String()]
			if !ok {
				// Fallback to default language if translation not available
				rendered, ok = l10nRenders[th.defaultLanguage]
				if !ok {
					http.Error(w, "internal error", http.StatusInternalServerError)
					return
				}
			}

			th.log.V(1).Info("serving announcement page", "language", l.String())

			w.Header().Set("Content-Language", l.String())
			w.Header().Set("Content-Type", "text/html")

			_, err := w.Write([]byte(rendered))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		}

		mux.HandleFunc("GET /", handler)
		mux.HandleFunc("GET /{l10n}/", handler)

		th.Mux = mux

		return
	}

	for _, ph := range pageHandlers {
		basePath := ph.Page.BasePath

		if basePath == "" {
			th.log.V(1).Info("somehow page has empty basePath, skipping", "page", ph.Name)
			continue
		}

		l10nRenders := th.L10nRendersLocked(ph, nil)

		handler := func(w http.ResponseWriter, r *http.Request) {
			// variables captured in scope of handler
			name := ph.Name
			basePath := ph.Page.BasePath
			l10nRenders := l10nRenders
			th := th

			l := kdexhttp.GetLang(r, th.defaultLanguage, th.Translations.Languages())

			rendered, ok := l10nRenders[l.String()]

			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}

			th.log.V(1).Info("serving", "page", name, "basePath", basePath, "language", l.String())

			w.Header().Set("Content-Language", l.String())
			w.Header().Set("Content-Type", "text/html")

			_, err := w.Write([]byte(rendered))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		}

		mux.HandleFunc("GET "+basePath, handler)
		mux.HandleFunc("GET /{l10n}"+basePath, handler)

		patternPath := ph.Page.PatternPath
		if patternPath != "" {
			mux.HandleFunc("GET "+patternPath, handler)
			mux.HandleFunc("GET /{l10n}"+patternPath, handler)
		}
	}

	th.Mux = mux
}

func (th *HostHandler) RemoveTranslation(name string) {
	th.log.V(1).Info("delete translation", "translation", name)
	th.mu.Lock()
	delete(th.translationResources, name)
	th.mu.Unlock()

	th.RebuildMux() // Called after lock is released
}

func (th *HostHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	th.mu.RLock()
	defer th.mu.RUnlock()
	if th.Mux != nil {
		th.Mux.ServeHTTP(w, r)
	} else {
		http.NotFound(w, r)
	}
}

func (th *HostHandler) SetHost(
	host *kdexv1alpha1.KDexHostSpec,
	assets []kdexv1alpha1.Asset,
	scriptLibraries []kdexv1alpha1.KDexScriptLibrarySpec,
	importmap string,
) {
	th.mu.Lock()
	th.defaultLanguage = host.DefaultLang
	th.host = host
	th.ScriptLibraries = scriptLibraries
	th.assets = assets
	th.importmap = importmap
	th.mu.Unlock()
	th.RebuildMux()
}

func (th *HostHandler) availableLanguagesLocked() []string {
	var availableLangs []string

	if th.Translations != nil {
		for _, tag := range th.Translations.Languages() {
			availableLangs = append(availableLangs, tag.String())
		}
	}

	return availableLangs
}

func (th *HostHandler) messagePrinterLocked(tag language.Tag) *message.Printer {
	return message.NewPrinter(
		tag,
		message.Catalog(th.Translations),
	)
}

func (th *HostHandler) muxWithDefaultsLocked() *http.ServeMux {
	mux := http.NewServeMux()

	handler := func(w http.ResponseWriter, r *http.Request) {
		th.mu.RLock()
		defer th.mu.RUnlock()

		basePath := "/" + r.PathValue("basePathMinusLeadingSlash")
		l10n := r.PathValue("l10n")
		navKey := r.PathValue("navKey")

		th.log.V(1).Info("generating navigation", "basePath", basePath, "l10n", l10n, "navKey", navKey)

		var pageHandler *page.PageHandler

		for _, ph := range th.Pages.List() {
			if ph.Page.BasePath == basePath {
				pageHandler = &ph
				break
			}
		}

		if pageHandler == nil {
			http.NotFound(w, r)
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
			http.NotFound(w, r)
			return
		}

		langTag := language.Make(l10n)
		if langTag.IsRoot() {
			langTag = language.Make(th.defaultLanguage)
		}

		rootEntry := &render.PageEntry{}
		th.Pages.BuildMenuEntries(rootEntry, &langTag, langTag.String() == th.defaultLanguage, nil)
		pageMap := *rootEntry.Children

		renderer := render.Renderer{
			BasePath:        pageHandler.Page.BasePath,
			BrandName:       th.host.BrandName,
			DefaultLanguage: th.defaultLanguage,
			Language:        langTag.String(),
			Languages:       th.availableLanguagesLocked(),
			LastModified:    time.Now(),
			MessagePrinter:  th.messagePrinterLocked(langTag),
			Organization:    th.host.Organization,
			PageMap:         pageMap,
			PatternPath:     pageHandler.Page.PatternPath,
			Title:           pageHandler.Page.Label,
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
	}

	mux.HandleFunc("GET /~/navigation/{navKey}/{l10n}/{basePathMinusLeadingSlash...}", handler)

	handler = func(w http.ResponseWriter, r *http.Request) {
		th.log.V(1).Info("unimplemented handler", "path", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, err := fmt.Fprintf(w, `{"path": "%s", "message": "Nothing here yet..."}`, r.URL.Path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}

	mux.HandleFunc("GET /~/check/", handler)
	mux.HandleFunc("GET /~/oauth/", handler)
	mux.HandleFunc("GET /~/navigation", handler)
	mux.HandleFunc("GET /~/state", handler)

	return mux
}

func (th *HostHandler) rebuildTranslationsLocked() {
	catalogBuilder := catalog.NewBuilder()

	if err := catalogBuilder.SetString(language.Make(th.defaultLanguage), "_", "_"); err != nil {
		th.log.Error(err, "failed to add placeholder translation")
	}

	// Add default translations for announcement page
	th.addDefaultAnnouncementTranslationsLocked(catalogBuilder)

	for _, translation := range th.translationResources {
		for name, tr := range translation.Translations {
			for key, value := range tr.KeysAndValues {
				if err := catalogBuilder.SetString(language.Make(tr.Lang), key, value); err != nil {
					th.log.Error(err, "failed to set translation", "translation", name, "lang", tr.Lang, "key", key, "value", value)
				}
			}
		}
	}

	th.Translations = catalogBuilder
}

func (th *HostHandler) addDefaultAnnouncementTranslationsLocked(catalogBuilder *catalog.Builder) {
	// English translations
	_ = catalogBuilder.SetString(language.English, "announcement.title", "Welcome to %s")
	_ = catalogBuilder.SetString(language.English, "announcement.message", "This host is ready to serve requests, but no pages have been deployed yet. Please deploy pages to start serving content.")
	_ = catalogBuilder.SetString(language.English, "announcement.organization", "Organization: %s")

	// Spanish translations
	_ = catalogBuilder.SetString(language.Spanish, "announcement.title", "Bienvenido a %s")
	_ = catalogBuilder.SetString(language.Spanish, "announcement.message", "Este host está listo para servir solicitudes, pero aún no se han desplegado páginas. Por favor, despliegue páginas para comenzar a servir contenido.")
	_ = catalogBuilder.SetString(language.Spanish, "announcement.organization", "Organización: %s")

	// French translations
	_ = catalogBuilder.SetString(language.French, "announcement.title", "Bienvenue sur %s")
	_ = catalogBuilder.SetString(language.French, "announcement.message", "Ce serveur est prêt à traiter les requêtes, mais aucune page n'a encore été déployée. Veuillez déployer des pages pour commencer à servir du contenu.")
	_ = catalogBuilder.SetString(language.French, "announcement.organization", "Organisation : %s")

	// German translations
	_ = catalogBuilder.SetString(language.German, "announcement.title", "Willkommen bei %s")
	_ = catalogBuilder.SetString(language.German, "announcement.message", "Dieser Host ist bereit, Anfragen zu bearbeiten, aber es wurden noch keine Seiten bereitgestellt. Bitte stellen Sie Seiten bereit, um mit der Bereitstellung von Inhalten zu beginnen.")
	_ = catalogBuilder.SetString(language.German, "announcement.organization", "Organisation: %s")
}
