package store

import (
	"sync"

	"github.com/go-logr/logr"
	"golang.org/x/text/language"
	"k8s.io/apimachinery/pkg/api/resource"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/crds/render"
)

type PageStore struct {
	log      logr.Logger
	mu       sync.RWMutex
	onUpdate func()
	handlers map[string]PageHandler
}

func (s *PageStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.handlers)
}

func (s *PageStore) Delete(name string) {
	s.mu.Lock()
	delete(s.handlers, name)
	s.mu.Unlock()
	if s.onUpdate != nil {
		s.onUpdate()
	}
}

func (s *PageStore) Get(name string) (PageHandler, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	page, ok := s.handlers[name]
	return page, ok
}

func (s *PageStore) List() []PageHandler {
	s.mu.RLock()
	defer s.mu.RUnlock()
	pages := []PageHandler{}
	for _, page := range s.handlers {
		pages = append(pages, page)
	}
	return pages
}

func (s *PageStore) Set(handler PageHandler) {
	s.log.Info("set render page", "name", handler.Page.Name)
	s.mu.Lock()
	s.handlers[handler.Page.Name] = handler
	s.mu.Unlock()
	if s.onUpdate != nil {
		s.onUpdate()
	}
}

func (s *PageStore) BuildMenuEntries(
	entry *render.PageEntry,
	l *language.Tag,
	isDefaultLanguage bool,
	parent *kdexv1alpha1.KDexPageBinding,
) {
	for _, handler := range s.List() {
		page := handler.Page

		if (parent == nil && page.Spec.ParentPageRef == nil) ||
			(parent != nil && page.Spec.ParentPageRef != nil &&
				parent.Name == page.Spec.ParentPageRef.Name) {

			if parent != nil && parent.Name == page.Name {
				continue
			}

			if entry.Children == nil {
				entry.Children = &map[string]*render.PageEntry{}
			}

			label := page.Spec.Label
			href := "/" + l.String() + page.Spec.BasePath

			pageEntry := render.PageEntry{
				Href:   href,
				Label:  label,
				Name:   page.Name,
				Weight: resource.MustParse("0"),
			}

			if page.Spec.NavigationHints != nil {
				pageEntry.Icon = page.Spec.NavigationHints.Icon
				pageEntry.Weight = page.Spec.NavigationHints.Weight
			}

			(*entry.Children)[label] = &pageEntry

			s.BuildMenuEntries(&pageEntry, l, isDefaultLanguage, page)
		}
	}
}
