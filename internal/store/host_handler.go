package store

import (
	"net/http"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/message/catalog"
	kdextemplate "kdex.dev/crds/api/template"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	kdexhttp "kdex.dev/web/internal/http"
	"kdex.dev/web/internal/render"
)

type HostHandler struct {
	Host                 kdexv1alpha1.MicroFrontEndHost
	Mux                  *http.ServeMux
	RenderPages          *RenderPageStore
	Translations         *catalog.Builder
	defaultLanguage      string
	log                  logr.Logger
	mu                   sync.RWMutex
	translationResources map[string]kdexv1alpha1.MicroFrontEndTranslation
}

func NewHostHandler(
	host kdexv1alpha1.MicroFrontEndHost,
	log logr.Logger,
) *HostHandler {
	th := &HostHandler{
		Host:                 host,
		log:                  log,
		translationResources: map[string]kdexv1alpha1.MicroFrontEndTranslation{},
	}
	th.updateHostDependentFields()
	rps := &RenderPageStore{
		host:     host,
		handlers: map[string]RenderPageHandler{},
		onUpdate: th.RebuildMux,
	}
	th.RenderPages = rps
	th.RebuildMux()
	return th
}

func (th *HostHandler) AddOrUpdateTranslation(translation kdexv1alpha1.MicroFrontEndTranslation) {
	th.mu.Lock()
	th.translationResources[translation.Name] = translation
	th.rebuildTranslationsLocked()
	th.mu.Unlock()

	th.RebuildMux() // Called after lock is released
}

func (th *HostHandler) L10nRenderLocked(
	handler RenderPageHandler,
	pageMap *map[string]*kdextemplate.PageEntry,
	l language.Tag,
) (string, error) {
	page := handler.Page

	renderer := render.Renderer{
		Languages:      th.availableLanguagesLocked(),
		RenderTime:     time.Now(),
		FootScript:     "",
		HeadScript:     "",
		Language:       l.String(),
		PageMap:        pageMap,
		MessagePrinter: th.messagePrinterLocked(l),
		Meta:           th.Host.Spec.BaseMeta,
		Organization:   th.Host.Spec.Organization,
		Stylesheet:     handler.StylesheetToString(),
	}

	return renderer.RenderPage(render.Page{
		Contents:        page.Spec.PageComponents.Contents,
		Footer:          page.Spec.PageComponents.Footer,
		Header:          page.Spec.PageComponents.Header,
		Label:           page.Spec.PageComponents.Title,
		Navigations:     page.Spec.PageComponents.Navigations,
		TemplateContent: page.Spec.PageComponents.PrimaryTemplate,
		TemplateName:    page.Name,
	})
}

func (th *HostHandler) L10nRendersLocked(
	handler RenderPageHandler,
	pageMaps map[language.Tag]*map[string]*kdextemplate.PageEntry,
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

func (th *HostHandler) RebuildMux() {
	th.mu.Lock()
	defer th.mu.Unlock()

	mux := http.NewServeMux()

	l10nPageMaps := th.generatePageMapsLocked()

	for _, handler := range th.RenderPages.List() {
		page := handler.Page

		l10nRenders := th.L10nRendersLocked(handler, l10nPageMaps)

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

		mux.HandleFunc("GET "+page.Spec.Paths.BasePath, handler)
		mux.HandleFunc("GET /{l10n}"+page.Spec.Paths.BasePath, handler)

		if page.Spec.Paths.PatternPath != "" {
			mux.HandleFunc("GET "+page.Spec.Paths.PatternPath, handler)
			mux.HandleFunc("GET /{l10n}"+page.Spec.Paths.PatternPath, handler)
		}
	}

	th.Mux = mux
}

func (th *HostHandler) RemoveTranslation(translation kdexv1alpha1.MicroFrontEndTranslation) {
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

func (th *HostHandler) SetHost(host kdexv1alpha1.MicroFrontEndHost) {
	th.mu.Lock()
	th.Host = host
	th.updateHostDependentFields()
	th.mu.Unlock() // <-- Release lock BEFORE calling RebuildMux

	th.RebuildMux()
}

// The public SetTranslations method should not be used internally anymore
// if it's called from a locked context.
func (th *HostHandler) SetTranslations(translations *catalog.Builder) {
	if translations == nil {
		return
	}
	th.mu.Lock()
	th.setTranslationsLocked(translations)
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

func (th *HostHandler) generatePageMapsLocked() map[language.Tag]*map[string]*kdextemplate.PageEntry {
	l10nPageMaps := map[language.Tag]*map[string]*kdextemplate.PageEntry{}

	for _, l := range th.Translations.Languages() {
		rootEntry := &kdextemplate.PageEntry{}
		th.RenderPages.BuildMenuEntries(
			rootEntry, &l, th.messagePrinterLocked(l), l.String() == th.defaultLanguage, nil)
		l10nPageMaps[l] = rootEntry.Children
	}

	return l10nPageMaps
}

func (th *HostHandler) messagePrinterLocked(tag language.Tag) *message.Printer {
	return message.NewPrinter(
		tag,
		message.Catalog(th.Translations),
	)
}

func (th *HostHandler) rebuildTranslationsLocked() {
	catalogBuilder := catalog.NewBuilder()

	if err := catalogBuilder.SetString(language.Make(th.defaultLanguage), "_", "_"); err != nil {
		th.log.Error(err, "failed to add default placeholder translation")
	}

	for _, translation := range th.translationResources {
		for _, tr := range translation.Spec.Translations {
			for key, value := range tr.KeysAndValues {
				if err := catalogBuilder.SetString(language.Make(tr.Lang), key, value); err != nil {
					th.log.Error(err, "failed to set translation string", "translation", translation.Name, "lang", tr.Lang, "key", key)
					continue
				}
			}
		}
	}

	th.setTranslationsLocked(catalogBuilder)
}

func (th *HostHandler) setTranslationsLocked(translations *catalog.Builder) {
	if translations == nil {
		return
	}
	th.Translations = translations
}

func (th *HostHandler) updateHostDependentFields() {
	defaultLang := "en"
	if th.Host.Spec.DefaultLang != "" {
		defaultLang = th.Host.Spec.DefaultLang
	}
	th.defaultLanguage = defaultLang

	catalogBuilder := catalog.NewBuilder()
	if err := catalogBuilder.SetString(language.Make(th.defaultLanguage), "_", "_"); err != nil {
		th.log.Error(err, "failed to add default placeholder translation")
	}
	th.Translations = catalogBuilder
}
