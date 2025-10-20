package store

import (
	"bytes"
	"net/http"
	"sync"
	"time"

	"github.com/go-logr/logr"
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

func (th *HostHandler) RemoveTranslation(translation kdexv1alpha1.MicroFrontEndTranslation) {
	th.mu.Lock()
	delete(th.translationResources, translation.Name)
	th.rebuildTranslationsLocked()
	th.mu.Unlock()

	th.RebuildMux() // Called after lock is released
}

// rebuildTranslationsLocked must be called with a write lock held.
// It does not acquire any locks itself.
func (th *HostHandler) rebuildTranslationsLocked() {
	catalogBuilder := catalog.NewBuilder()

	if err := catalogBuilder.SetString(language.Make(th.defaultLang), "_", "_"); err != nil {
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

// setTranslationsLocked must be called with a write lock held.
// It does not acquire any locks itself.
func (th *HostHandler) setTranslationsLocked(translations *catalog.Builder) {
	if translations == nil {
		return
	}
	th.Translations = translations
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

func (th *HostHandler) updateHostDependentFields() {
	defaultLang := "en"
	if th.Host.Spec.DefaultLang != "" {
		defaultLang = th.Host.Spec.DefaultLang
	}
	th.defaultLang = defaultLang

	catalogBuilder := catalog.NewBuilder()
	if err := catalogBuilder.SetString(language.Make(th.defaultLang), "_", "_"); err != nil {
		th.log.Error(err, "failed to add default placeholder translation")
	}
	th.Translations = catalogBuilder
}

func (th *HostHandler) RebuildMux() {
	th.mu.Lock()
	defer th.mu.Unlock()

	mux := http.NewServeMux()

	rootEntry := &menu.MenuEntry{}
	th.RenderPages.BuildMenuEntries(rootEntry, nil)

	for _, handler := range th.RenderPages.List() {
		page := handler.Page

		l10nRenders := th.L10nRenders(handler, rootEntry.Children)

		handler := func(w http.ResponseWriter, r *http.Request) {
			lang := kdexhttp.GetLang(r, th.defaultLang, th.Translations.Languages())

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
	handler RenderPageHandler,
	menuEntries *map[string]*menu.MenuEntry,
	lang language.Tag,
) (string, error) {
	var messagePrinter *message.Printer

	if th.Translations != nil {
		messagePrinter = message.NewPrinter(
			lang,
			message.Catalog(th.Translations),
		)
	}

	page := handler.Page

	var styleBuffer bytes.Buffer

	if handler.Stylesheet != nil {
		for _, item := range handler.Stylesheet.Spec.StyleItems {
			if item.LinkHref != "" {
				styleBuffer.WriteString(`<link`)
				for key, value := range item.Attributes {
					if key == "href" || key == "src" {
						continue
					}
					styleBuffer.WriteRune(' ')
					styleBuffer.WriteString(key)
					styleBuffer.WriteString(`="`)
					styleBuffer.WriteString(value)
					styleBuffer.WriteRune('"')
				}
				styleBuffer.WriteString(` href="`)
				styleBuffer.WriteString(item.LinkHref)
				styleBuffer.WriteString(`"/>`)
				styleBuffer.WriteRune('\n')
			} else if item.Style != "" {
				styleBuffer.WriteString(`<style`)
				for key, value := range item.Attributes {
					if key == "href" || key == "src" {
						continue
					}
					styleBuffer.WriteRune(' ')
					styleBuffer.WriteString(key)
					styleBuffer.WriteString(`="`)
					styleBuffer.WriteString(value)
					styleBuffer.WriteRune('"')
				}
				styleBuffer.WriteRune('>')
				styleBuffer.WriteRune('\n')
				styleBuffer.WriteString(item.Style)
				styleBuffer.WriteString("</style>")
				styleBuffer.WriteRune('\n')
			}
		}
	}

	renderer := render.Renderer{
		Date:           time.Now(),
		FootScript:     "",
		HeadScript:     "",
		Lang:           lang.String(),
		MenuEntries:    menuEntries,
		MessagePrinter: messagePrinter,
		Meta:           th.Host.Spec.BaseMeta,
		Organization:   th.Host.Spec.Organization,
		Stylesheet:     styleBuffer.String(),
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
	handler RenderPageHandler,
	children *map[string]*menu.MenuEntry,
) map[string]string {
	l10nRenders := make(map[string]string)
	for _, lang := range th.Translations.Languages() {
		rendered, err := th.L10nRender(handler, children, lang)
		if err != nil {
			th.log.Error(err, "failed to render page for language", "page", handler.Page.Name, "lang", lang)
			continue
		}
		l10nRenders[lang.String()] = rendered
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
