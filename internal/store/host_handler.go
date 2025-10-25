package store

import (
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
		log:      log.WithName("render-page-store"),
		onUpdate: th.RebuildMux,
	}
	th.RenderPages = rps
	th.RebuildMux()
	return th
}

func (th *HostHandler) AddOrUpdateTranslation(translation kdexv1alpha1.MicroFrontEndTranslation) {
	th.log.Info("add or update translation")
	th.mu.Lock()
	th.translationResources[translation.Name] = translation
	th.rebuildTranslationsLocked()
	th.mu.Unlock()

	th.RebuildMux() // Called after lock is released
}

func (th *HostHandler) L10nRenderLocked(
	handler RenderPageHandler,
	pageMap *map[string]*render.PageEntry,
	l language.Tag,
) (string, error) {
	page := handler.Page

	renderer := render.Renderer{
		FootScript:     "",
		HeadScript:     "",
		Language:       l.String(),
		Languages:      th.availableLanguagesLocked(),
		LastModified:   time.Now(),
		MessagePrinter: th.messagePrinterLocked(l),
		Meta:           th.Host.Spec.BaseMeta,
		Organization:   th.Host.Spec.Organization,
		PageMap:        pageMap,
		StyleItems:     handler.Stylesheet.Spec.StyleItems,
	}

	return renderer.RenderPage(render.Page{
		Contents:        page.Spec.PageComponents.Contents,
		Footer:          page.Spec.PageComponents.Footer,
		Header:          page.Spec.PageComponents.Header,
		Title:           page.Spec.PageComponents.Title,
		Navigations:     page.Spec.PageComponents.Navigations,
		TemplateContent: page.Spec.PageComponents.PrimaryTemplate,
		TemplateName:    page.Name,
	})
}

func (th *HostHandler) L10nRendersLocked(
	handler RenderPageHandler,
	pageMaps map[language.Tag]*map[string]*render.PageEntry,
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
	th.log.Info("rebuilding mux")
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

		mux.HandleFunc("GET "+page.Spec.BasePath, handler)
		mux.HandleFunc("GET /{l10n}"+page.Spec.BasePath, handler)

		if page.Spec.PatternPath != "" {
			mux.HandleFunc("GET "+page.Spec.PatternPath, handler)
			mux.HandleFunc("GET /{l10n}"+page.Spec.PatternPath, handler)
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

func (th *HostHandler) generatePageMapsLocked() map[language.Tag]*map[string]*render.PageEntry {
	l10nPageMaps := map[language.Tag]*map[string]*render.PageEntry{}

	for _, l := range th.Translations.Languages() {
		rootEntry := &render.PageEntry{}
		th.RenderPages.BuildMenuEntries(rootEntry, &l, l.String() == th.defaultLanguage, nil)
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
