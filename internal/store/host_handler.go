package store

import (
	"net/http"
	"sync"
	"time"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/message/catalog"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	kdexhttp "kdex.dev/web/internal/http"
	"kdex.dev/web/internal/menu"
	"kdex.dev/web/internal/render"
)

type HostHandler struct {
	Host                 kdexv1alpha1.MicroFrontEndHost
	Mux                  *http.ServeMux
	RenderPages          *RenderPageStore
	Translations         *catalog.Builder
	defaultLang          string
	mu                   sync.RWMutex
	supportedLangs       []language.Tag
	translationResources map[string]kdexv1alpha1.MicroFrontEndTranslation
}

func NewHostHandler(
	host kdexv1alpha1.MicroFrontEndHost,
) *HostHandler {
	defaultLang := "en"

	if host.Spec.DefaultLang != "" {
		defaultLang = host.Spec.DefaultLang
	}

	supportedLangs := []language.Tag{}

	if len(host.Spec.SupportedLangs) > 0 {
		for _, lang := range host.Spec.SupportedLangs {
			supportedLangs = append(supportedLangs, language.Make(lang))
		}
	} else {
		supportedLangs = append(supportedLangs, language.Make(defaultLang))
	}

	th := &HostHandler{
		Host:                 host,
		defaultLang:          defaultLang,
		supportedLangs:       supportedLangs,
		translationResources: make(map[string]kdexv1alpha1.MicroFrontEndTranslation),
	}
	rps := &RenderPageStore{
		host:     host,
		pages:    make(map[string]kdexv1alpha1.MicroFrontEndRenderPage),
		onUpdate: th.RebuildMux,
	}
	th.RenderPages = rps
	th.RebuildMux()
	return th
}

func (th *HostHandler) AddOrUpdateTranslation(translation kdexv1alpha1.MicroFrontEndTranslation) {
	th.mu.Lock()
	defer th.mu.Unlock()
	th.translationResources[translation.Name] = translation
	th.rebuildTranslations()
}

func (th *HostHandler) RemoveTranslation(translation kdexv1alpha1.MicroFrontEndTranslation) {
	th.mu.Lock()
	defer th.mu.Unlock()
	delete(th.translationResources, translation.Name)
	th.rebuildTranslations()
}

func (th *HostHandler) rebuildTranslations() {
	catalogBuilder := catalog.NewBuilder()

	for _, translation := range th.translationResources {
		for _, tr := range translation.Spec.Translations {
			for key, value := range tr.KeysAndValues {
				if err := catalogBuilder.SetString(language.Make(tr.Lang), key, value); err != nil {
					// log something here...
					continue
				}
			}
		}
	}

	th.SetTranslations(catalogBuilder)
}

func (th *HostHandler) SetTranslations(translations *catalog.Builder) {
	if translations == nil {
		return
	}
	th.mu.Lock()
	th.Translations = translations
	th.mu.Unlock()
	th.RebuildMux()
}

func (th *HostHandler) RebuildMux() {
	th.mu.Lock()
	defer th.mu.Unlock()

	mux := http.NewServeMux()

	rootEntry := &menu.MenuEntry{}
	th.RenderPages.BuildMenuEntries(rootEntry, nil)

	pages := th.RenderPages.List()
	for i := range pages {
		page := pages[i]

		l10nRenders := th.L10nRenders(
			page, th.Host.Spec.SupportedLangs, rootEntry.Children)

		handler := func(w http.ResponseWriter, r *http.Request) {
			lang := kdexhttp.GetLang(r, th.defaultLang, th.supportedLangs)

			rendered, ok := l10nRenders[lang.String()]

			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}

			w.Header().Set("Content-Language", lang.String())
			w.Header().Set("Content-Type", "text/html")

			_, err := w.Write([]byte(rendered))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		}

		mux.HandleFunc("GET "+page.Spec.Path, handler)
	}

	th.Mux = mux
}

func (th *HostHandler) L10nRender(
	page kdexv1alpha1.MicroFrontEndRenderPage,
	menuEntries *map[string]*menu.MenuEntry,
	lang string,
) (string, error) {
	var messagePrinter *message.Printer

	if th.Translations != nil {
		messagePrinter = message.NewPrinter(
			language.Make(lang),
			message.Catalog(th.Translations),
		)
	}

	renderer := render.Renderer{
		Date:           time.Now(),
		FootScript:     "",
		HeadScript:     "",
		Lang:           lang,
		MenuEntries:    menuEntries,
		MessagePrinter: messagePrinter,
		Meta:           th.Host.Spec.BaseMeta,
		Organization:   th.Host.Spec.Organization,
		Stylesheet:     th.Host.Spec.Stylesheet,
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

func (th *HostHandler) L10nRenders(
	page kdexv1alpha1.MicroFrontEndRenderPage,
	langs []string,
	children *map[string]*menu.MenuEntry,
) map[string]string {
	l10nRenders := make(map[string]string)
	for _, lang := range th.Host.Spec.SupportedLangs {
		rendered, err := th.L10nRender(page, children, lang)
		if err != nil {
			// log something here...
			continue
		}
		l10nRenders[lang] = rendered
	}
	return l10nRenders
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
