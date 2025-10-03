package store

import (
	"net/http"
	"sync"
	"time"

	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/web/internal/menu"
	"kdex.dev/web/internal/render"
)

type TrackedHost struct {
	Host        kdexv1alpha1.MicroFrontEndHost
	Mux         *http.ServeMux
	RenderPages *RenderPageStore
	mu          sync.RWMutex
}

func NewTrackedHost(host kdexv1alpha1.MicroFrontEndHost) *TrackedHost {
	th := &TrackedHost{
		Host: host,
	}
	rps := &RenderPageStore{
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
	pages := th.RenderPages.List()
	for i := range pages {
		page := pages[i]

		handler := func(w http.ResponseWriter, r *http.Request) {
			lang := r.PathValue("lang")

			if lang != "" {
				lang = "en" // TODO: use the Hosts DefaultLang property
			}

			renderer := render.Renderer{
				Context:      r.Context(),
				Date:         time.Now(),
				FootScript:   "",
				HeadScript:   "",
				Lang:         lang,
				MenuEntries:  menu.ToMenuEntries(pages),
				Meta:         *th.Host.Spec.BaseMeta,
				Organization: th.Host.Spec.Organization,
				Request:      r,
				Stylesheet:   th.Host.Spec.Stylesheet,
			}

			actual, err := renderer.RenderPage(render.Page{
				Contents:        page.Spec.PageComponents.Contents,
				Footer:          page.Spec.PageComponents.Footer,
				Header:          page.Spec.PageComponents.Header,
				Label:           page.Spec.PageComponents.Title,
				Navigations:     page.Spec.PageComponents.Navigations,
				TemplateContent: page.Spec.PageComponents.PrimaryTemplate,
				TemplateName:    page.Name,
			})

			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "text/html")
			_, err = w.Write([]byte(actual))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		}

		mux.HandleFunc("GET "+page.Spec.Path, handler)
	}

	th.Mux = mux
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

type HostStore struct {
	mu    sync.RWMutex
	hosts map[string]*TrackedHost
}

func NewHostStore() *HostStore {
	return &HostStore{
		hosts: make(map[string]*TrackedHost),
	}
}

func (s *HostStore) Delete(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.hosts, name)
}

func (s *HostStore) Get(name string) (*TrackedHost, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	host, ok := s.hosts[name]
	return host, ok
}

func (s *HostStore) List() []*TrackedHost {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var hosts []*TrackedHost
	for _, host := range s.hosts {
		hosts = append(hosts, host)
	}
	return hosts
}

func (s *HostStore) Set(host *TrackedHost) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hosts[host.Host.Name] = host
}

type RenderPageStore struct {
	mu       sync.RWMutex
	onUpdate func()
	pages    map[string]kdexv1alpha1.MicroFrontEndRenderPage
}

func (s *RenderPageStore) Delete(name string) {
	s.mu.Lock()
	delete(s.pages, name)
	s.mu.Unlock()
	if s.onUpdate != nil {
		s.onUpdate()
	}
}

func (s *RenderPageStore) Get(name string) (kdexv1alpha1.MicroFrontEndRenderPage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	page, ok := s.pages[name]
	return page, ok
}

func (s *RenderPageStore) List() []kdexv1alpha1.MicroFrontEndRenderPage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var pages []kdexv1alpha1.MicroFrontEndRenderPage
	for _, page := range s.pages {
		pages = append(pages, page)
	}
	return pages
}

func (s *RenderPageStore) Set(page kdexv1alpha1.MicroFrontEndRenderPage) {
	s.mu.Lock()
	s.pages[page.Name] = page
	s.mu.Unlock()
	if s.onUpdate != nil {
		s.onUpdate()
	}
}
