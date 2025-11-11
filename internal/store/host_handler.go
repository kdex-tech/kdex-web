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
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type HostHandler struct {
	Mux                  *http.ServeMux
	Pages                *PageStore
	Translations         *catalog.Builder
	defaultLanguage      string
	host                 *kdexv1alpha1.KDexHost
	log                  logr.Logger
	mu                   sync.RWMutex
	scriptLibrary        *kdexv1alpha1.KDexScriptLibrary
	theme                *kdexv1alpha1.KDexTheme
	translationResources map[string]kdexv1alpha1.KDexTranslation
}

func NewHostHandler(
	log logr.Logger,
) *HostHandler {
	th := &HostHandler{
		defaultLanguage:      "en",
		log:                  log,
		translationResources: map[string]kdexv1alpha1.KDexTranslation{},
	}

	catalogBuilder := catalog.NewBuilder()
	if err := catalogBuilder.SetString(language.Make(th.defaultLanguage), "_", "_"); err != nil {
		th.log.Error(err, "failed to add default placeholder translation")
	}
	th.Translations = catalogBuilder

	rps := &PageStore{
		handlers: map[string]PageHandler{},
		log:      log.WithName("render-page-store"),
		onUpdate: th.RebuildMux,
	}
	th.Pages = rps
	th.RebuildMux()
	return th
}

func (th *HostHandler) AddOrUpdateTranslation(translation *kdexv1alpha1.KDexTranslation) {
	th.log.Info("add or update translation", "translation", translation.Name)
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
	return th.host.Spec.Routing.Domains
}

func (th *HostHandler) L10nRenderLocked(
	handler PageHandler,
	pageMap *map[string]*render.PageEntry,
	l language.Tag,
) (string, error) {
	page := handler.Page

	assets := []kdexv1alpha1.Asset{}

	if th.theme != nil {
		assets = append(assets, th.theme.Spec.Assets...)
	}

	renderer := render.Renderer{
		BasePath:        page.Spec.BasePath,
		Contents:        map[string]string{}, // TODO fix
		DefaultLanguage: th.defaultLanguage,
		Footer:          "", // TODO fix
		FootScript:      "", // TODO fix
		Header:          "", // TODO fix
		HeadScript:      "",
		Language:        l.String(),
		Languages:       th.availableLanguagesLocked(),
		LastModified:    time.Now(),
		MessagePrinter:  th.messagePrinterLocked(l),
		Meta:            th.host.Spec.BaseMeta,
		Navigations:     map[string]string{}, // TODO fix
		Organization:    th.host.Spec.Organization,
		PageMap:         pageMap,
		TemplateContent: "", // TODO fix
		TemplateName:    page.Name,
		Theme:           "", // TODO merge assets
		Title:           page.Spec.Label,
	}

	return renderer.RenderPage()
}

func (th *HostHandler) L10nRendersLocked(
	handler PageHandler,
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

	if th.host == nil {
		return
	}

	mux := http.NewServeMux()

	l10nPageMaps := th.generatePageMapsLocked()

	pageList := th.Pages.List()

	if len(pageList) == 0 {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			log := logf.FromContext(r.Context())

			log.Info("no pages found", "host", th.host.Name)

			http.NotFound(w, r)
		})
	}

	for _, handler := range pageList {
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

		if page.Spec.PatternPath != "" {
			mux.HandleFunc("GET "+page.Spec.Paths.PatternPath, handler)
			mux.HandleFunc("GET /{l10n}"+page.Spec.Paths.PatternPath, handler)
		}
	}

	th.Mux = mux
}

func (th *HostHandler) RemoveTranslation(translation kdexv1alpha1.KDexTranslation) {
	th.log.Info("delete translation", "translation", translation.Name)
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
	host *kdexv1alpha1.KDexHost,
	scriptLibrary *kdexv1alpha1.KDexScriptLibrary,
	theme *kdexv1alpha1.KDexTheme,
) {
	th.mu.Lock()
	th.defaultLanguage = host.Spec.DefaultLang
	th.host = host
	th.scriptLibrary = scriptLibrary
	th.theme = theme
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
		th.Pages.BuildMenuEntries(rootEntry, &l, l.String() == th.defaultLanguage, nil)
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
