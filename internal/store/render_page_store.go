package store

import (
	"sync"

	"github.com/go-logr/logr"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"k8s.io/apimachinery/pkg/api/resource"
	kdextemplate "kdex.dev/crds/api/template"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
)

type RenderPageStore struct {
	host     kdexv1alpha1.MicroFrontEndHost
	log      logr.Logger
	mu       sync.RWMutex
	onUpdate func()
	handlers map[string]RenderPageHandler
}

func (s *RenderPageStore) Delete(name string) {
	s.mu.Lock()
	delete(s.handlers, name)
	s.mu.Unlock()
	if s.onUpdate != nil {
		s.onUpdate()
	}
}

func (s *RenderPageStore) Get(name string) (RenderPageHandler, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	page, ok := s.handlers[name]
	return page, ok
}

func (s *RenderPageStore) List() []RenderPageHandler {
	s.mu.RLock()
	defer s.mu.RUnlock()
	pages := []RenderPageHandler{}
	for _, page := range s.handlers {
		pages = append(pages, page)
	}
	return pages
}

func (s *RenderPageStore) Set(handler RenderPageHandler) {
	s.log.Info("set render page", "name", handler.Page.Name)
	s.mu.Lock()
	s.handlers[handler.Page.Name] = handler
	s.mu.Unlock()
	if s.onUpdate != nil {
		s.onUpdate()
	}
}

func (s *RenderPageStore) BuildMenuEntries(
	entry *kdextemplate.PageEntry,
	l *language.Tag,
	messagePrinter *message.Printer,
	isDefaultLanguage bool,
	parent *kdexv1alpha1.MicroFrontEndRenderPage,
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
				entry.Children = &map[string]*kdextemplate.PageEntry{}
			}

			label := messagePrinter.Sprintf(page.Spec.PageComponents.Title)
			href := page.Spec.Paths.BasePath

			if !isDefaultLanguage {
				href = "/" + l.String() + href
			}

			pageEntry := kdextemplate.PageEntry{
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

			s.BuildMenuEntries(
				&pageEntry, l, messagePrinter, isDefaultLanguage, &page)
		}
	}
}
