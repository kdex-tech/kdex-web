package page

import (
	"sync"

	"github.com/go-logr/logr"
	"golang.org/x/text/language"
	"k8s.io/apimachinery/pkg/api/resource"
	"kdex.dev/crds/render"
)

type PageStore struct {
	handlers map[string]PageHandler
	log      logr.Logger
	mu       sync.RWMutex
	onUpdate func()
}

func NewPageStore(host string, onUpdate func(), log logr.Logger) *PageStore {
	return &PageStore{
		handlers: map[string]PageHandler{},
		onUpdate: onUpdate,
		log:      log,
	}
}

func (s *PageStore) Count() int {
	s.log.V(1).Info("count")
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.handlers)
}

func (s *PageStore) Delete(name string) {
	s.log.V(1).Info("delete", "name", name)
	s.mu.Lock()
	delete(s.handlers, name)
	s.mu.Unlock()
	if s.onUpdate != nil {
		s.onUpdate()
	}
}

func (s *PageStore) Get(name string) (PageHandler, bool) {
	s.log.V(1).Info("get", "name", name)
	s.mu.RLock()
	defer s.mu.RUnlock()
	page, ok := s.handlers[name]
	return page, ok
}

func (s *PageStore) List() []PageHandler {
	s.log.V(1).Info("list")
	s.mu.RLock()
	defer s.mu.RUnlock()
	pages := []PageHandler{}
	for _, page := range s.handlers {
		pages = append(pages, page)
	}
	return pages
}

func (s *PageStore) Set(handler PageHandler) {
	s.log.V(1).Info("set", "name", handler.Name)
	s.mu.Lock()
	s.handlers[handler.Name] = handler
	s.mu.Unlock()
	if s.onUpdate != nil {
		s.onUpdate()
	}
}

func (s *PageStore) BuildMenuEntries(
	entry *render.PageEntry,
	l *language.Tag,
	isDefaultLanguage bool,
	parent *PageHandler,
) {
	for _, handler := range s.List() {
		page := handler.Page

		if (parent == nil && page.ParentPageRef == nil) ||
			(parent != nil && page.ParentPageRef != nil &&
				parent.Name == page.ParentPageRef.Name) {

			if parent != nil && parent.Name == handler.Name {
				continue
			}

			if entry.Children == nil {
				entry.Children = &map[string]interface{}{}
			}

			label := page.Label

			href := page.BasePath
			if !isDefaultLanguage {
				href = "/" + l.String() + page.BasePath
			}

			pageEntry := render.PageEntry{
				BasePath: page.BasePath,
				Href:     href,
				Label:    label,
				Name:     handler.Name,
				Weight:   resource.MustParse("0"),
			}

			if page.NavigationHints != nil {
				pageEntry.Icon = page.NavigationHints.Icon
				pageEntry.Weight = page.NavigationHints.Weight
			}

			s.BuildMenuEntries(&pageEntry, l, isDefaultLanguage, &handler)

			(*entry.Children)[label] = pageEntry
		}
	}
}
