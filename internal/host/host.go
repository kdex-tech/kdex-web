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
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	kdexUIMetaTemplate = `<meta
  name="kdex-ui"
  data-page-basepath="%s"
  data-navigation-endpoint="/~/navigation/{name}/{l10n}/{basePathMinusLeadingSlash...}"
  data-page-patternpath="%s"
/>
`
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
	translationResources map[string]kdexv1alpha1.KDexTranslation
}

func NewHostHandler(
	name string,
	log logr.Logger,
) *HostHandler {
	th := &HostHandler{
		Name:                 name,
		defaultLanguage:      "en",
		log:                  log.WithName("hostHandler"),
		translationResources: map[string]kdexv1alpha1.KDexTranslation{},
	}

	catalogBuilder := catalog.NewBuilder()
	if err := catalogBuilder.SetString(language.Make(th.defaultLanguage), "_", "_"); err != nil {
		th.log.Error(err, "failed to add default placeholder translation")
	}
	th.Translations = catalogBuilder

	rps := &page.PageStore{
		Handlers: map[string]page.PageHandler{},
		Log:      log.WithName("pageStore"),
		OnUpdate: th.RebuildMux,
	}
	th.Pages = rps
	th.RebuildMux()
	return th
}

func (th *HostHandler) AddOrUpdateTranslation(translation *kdexv1alpha1.KDexTranslation) {
	if translation == nil {
		return
	}
	th.log.V(1).Info("add or update translation", "translation", translation.Name)
	th.mu.Lock()
	th.translationResources[translation.Name] = *translation
	th.rebuildTranslationsLocked()
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
			buffer.WriteString(script.ToScriptTag(true))
			separator = "\n"
		}
	}
	for _, scriptLibrary := range handler.ScriptLibraries {
		for _, script := range scriptLibrary.Scripts {
			buffer.WriteString(separator)
			buffer.WriteString(script.ToScriptTag(true))
			separator = "\n"
		}
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
			buffer.WriteString(script.ToScriptTag(false))
			separator = "\n"
		}
	}
	for _, scriptLibrary := range handler.ScriptLibraries {
		for _, script := range scriptLibrary.Scripts {
			buffer.WriteString(separator)
			buffer.WriteString(script.ToScriptTag(false))
			separator = "\n"
		}
	}

	return buffer.String()
}

func (th *HostHandler) L10nRenderLocked(
	handler page.PageHandler,
	pageMap map[string]interface{},
	l language.Tag,
) (string, error) {
	renderer := render.Renderer{
		BasePath:        handler.Page.Spec.BasePath,
		BrandName:       th.host.BrandName,
		Contents:        handler.ContentToHTMLMap(),
		DefaultLanguage: th.defaultLanguage,
		Footer:          handler.FooterToHTML(),
		FootScript:      th.FootScriptToHTML(handler),
		Header:          handler.HeaderToHTML(),
		HeadScript:      th.HeadScriptToHTML(handler),
		Language:        l.String(),
		Languages:       th.availableLanguagesLocked(),
		LastModified:    time.Now(),
		MessagePrinter:  th.messagePrinterLocked(l),
		Meta:            th.MetaToString(handler),
		Navigations:     handler.NavigationToHTMLMap(),
		Organization:    th.host.Organization,
		PageMap:         pageMap,
		PatternPath:     handler.Page.Spec.PatternPath,
		TemplateContent: handler.Archetype.Content,
		TemplateName:    handler.Page.Name,
		Theme:           th.ThemeToString(),
		Title:           handler.Page.Spec.Label,
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
			th.log.Error(err, "failed to render page for language", "page", handler.Page.Name, "language", l)
			continue
		}
		l10nRenders[l.String()] = rendered
	}
	return l10nRenders
}

func (th *HostHandler) MetaToString(handler page.PageHandler) string {
	var buffer bytes.Buffer

	if th.host.BaseMeta != "" {
		buffer.WriteString(th.host.BaseMeta)
		buffer.WriteRune('\n')
	}

	fmt.Fprintf(
		&buffer,
		kdexUIMetaTemplate,
		handler.Page.Spec.BasePath,
		handler.Page.Spec.PatternPath,
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

	mux := th.muxWithDefaultsLocked()

	// l10nPageMaps := th.generatePageMapsLocked()

	pageList := th.Pages.List()

	if len(pageList) == 0 {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			log := logf.FromContext(r.Context())

			log.V(1).Info("no pages found", "host", th.Name)

			http.NotFound(w, r)
		})

		th.Mux = mux

		return
	}

	for _, handler := range pageList {
		p := handler.Page

		if p.Spec.BasePath == "" {
			th.log.V(1).Info("page has empty basePath somehow...", "page", p)
			continue
		}

		l10nRenders := th.L10nRendersLocked(handler, nil)

		handler := func(w http.ResponseWriter, r *http.Request) {
			l := kdexhttp.GetLang(r, th.defaultLanguage, th.Translations.Languages())

			rendered, ok := l10nRenders[l.String()]

			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}

			w.Header().Set("Content-Language", l.String())
			w.Header().Set("Content-Type", "text/html")

			_, err := w.Write([]byte(rendered))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		}

		mux.HandleFunc("GET "+p.Spec.BasePath, handler)
		mux.HandleFunc("GET /{l10n}"+p.Spec.BasePath, handler)

		if p.Spec.PatternPath != "" {
			mux.HandleFunc("GET "+p.Spec.PatternPath, handler)
			mux.HandleFunc("GET /{l10n}"+p.Spec.PatternPath, handler)
		}
	}

	th.Mux = mux
}

func (th *HostHandler) RemoveTranslation(translation kdexv1alpha1.KDexTranslation) {
	th.log.V(1).Info("delete translation", "translation", translation.Name)
	th.mu.Lock()
	delete(th.translationResources, translation.Name)
	th.rebuildTranslationsLocked()
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

func (th *HostHandler) ThemeToString() string {
	var buffer bytes.Buffer
	separator := ""
	for _, asset := range th.assets {
		buffer.WriteString(separator)
		buffer.WriteString(asset.String())
		separator = "\n"
	}

	return buffer.String()
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

// func (th *HostHandler) generatePageMapsLocked() map[language.Tag]*map[string]*render.PageEntry {
// 	l10nPageMaps := map[language.Tag]*map[string]*render.PageEntry{}

// 	for _, l := range th.Translations.Languages() {
// 		rootEntry := &render.PageEntry{}
// 		th.Pages.BuildMenuEntries(rootEntry, &l, l.String() == th.defaultLanguage, nil)
// 		l10nPageMaps[l] = rootEntry.Children
// 	}

// 	return l10nPageMaps
// }

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
			if ph.Page.Spec.BasePath == basePath {
				pageHandler = &ph
				break
			}
		}

		if pageHandler == nil {
			http.NotFound(w, r)
			return
		}

		var nav *kdexv1alpha1.KDexPageNavigationSpec

		for key, n := range pageHandler.Navigations {
			if key == navKey {
				nav = n.Spec
				break
			}
		}

		if nav == nil {
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
			BasePath:        pageHandler.Page.Spec.BasePath,
			BrandName:       th.host.BrandName,
			DefaultLanguage: th.defaultLanguage,
			Language:        langTag.String(),
			Languages:       th.availableLanguagesLocked(),
			LastModified:    time.Now(),
			MessagePrinter:  th.messagePrinterLocked(langTag),
			Organization:    th.host.Organization,
			PageMap:         pageMap,
			PatternPath:     pageHandler.Page.Spec.PatternPath,
			Title:           pageHandler.Page.Spec.Label,
		}

		templateData, err := renderer.TemplateData()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		rendered, err := renderer.RenderOne(navKey, nav.Content, templateData)
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

	for _, translation := range th.translationResources {
		for _, tr := range translation.Spec.Translations {
			for key, value := range tr.KeysAndValues {
				if err := catalogBuilder.SetString(language.Make(tr.Lang), key, value); err != nil {
					th.log.Error(err, "failed to set translation", "translation", translation.Name, "lang", tr.Lang, "key", key, "value", value)
				}
			}
		}
	}

	th.Translations = catalogBuilder
}
