package store

import (
	"net/http"
	"sync"
	"time"

	"golang.org/x/text/language"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	kdexhttp "kdex.dev/web/internal/http"
	"kdex.dev/web/internal/menu"
	"kdex.dev/web/internal/render"
)

type TrackedHost struct {
	Host           kdexv1alpha1.MicroFrontEndHost
	Mux            *http.ServeMux
	RenderPages    *RenderPageStore
	defaultLang    string
	supportedLangs []language.Tag
	mu             sync.RWMutex
}

func NewTrackedHost(host kdexv1alpha1.MicroFrontEndHost) *TrackedHost {
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

	th := &TrackedHost{
		Host:           host,
		defaultLang:    defaultLang,
		supportedLangs: supportedLangs,
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

func (th *TrackedHost) RebuildMux() {
	th.mu.Lock()
	defer th.mu.Unlock()

	mux := http.NewServeMux()

	rootEntry := &menu.MenuEntry{}
	th.RenderPages.BuildMenuEntries(rootEntry, nil)

	pages := th.RenderPages.List()
	for i := range pages {
		page := pages[i]

		l10nRenders := make(map[string]string)
		for _, lang := range th.Host.Spec.SupportedLangs {
			rendered, err := th.RenderPage(page, lang, rootEntry.Children)
			if err != nil {
				// log something here...
				continue
			}
			l10nRenders[lang] = rendered
		}

		handler := func(w http.ResponseWriter, r *http.Request) {
			lang := kdexhttp.GetLang(r, th.defaultLang, th.supportedLangs)

			rendered, ok := l10nRenders[lang.String()]

			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}

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

func (th *TrackedHost) RenderPage(
	page kdexv1alpha1.MicroFrontEndRenderPage,
	lang string,
	menuEntries *map[string]*menu.MenuEntry,
) (string, error) {
	renderer := render.Renderer{
		Date:         time.Now(),
		FootScript:   "",
		HeadScript:   "",
		Lang:         lang,
		MenuEntries:  menuEntries,
		Meta:         th.Host.Spec.BaseMeta,
		Organization: th.Host.Spec.Organization,
		Stylesheet:   th.Host.Spec.Stylesheet,
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

func (th *TrackedHost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	th.mu.RLock()
	defer th.mu.RUnlock()
	if th.Mux != nil {
		th.Mux.ServeHTTP(w, r)
	} else {
		http.NotFound(w, r)
	}
}
