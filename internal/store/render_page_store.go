package store

import (
	"sync"

	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/web/internal/menu"
)

type RenderPageStore struct {
	host     kdexv1alpha1.MicroFrontEndHost
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
	s.mu.Lock()
	s.handlers[handler.Page.Name] = handler
	s.mu.Unlock()
	if s.onUpdate != nil {
		s.onUpdate()
	}
}

func (s *RenderPageStore) BuildMenuEntries(
	entry *menu.MenuEntry,
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
				entry.Children = &map[string]*menu.MenuEntry{}
			}

			label := page.Spec.PageComponents.Title

			menuEntry := menu.MenuEntry{
				Name: page.Name,
				Path: page.Spec.Path,
			}

			if page.Spec.NavigationHints != nil {
				menuEntry.Icon = page.Spec.NavigationHints.Icon
				menuEntry.Weight = page.Spec.NavigationHints.Weight
			}

			(*entry.Children)[label] = &menuEntry

			s.BuildMenuEntries(&menuEntry, &page)
		}
	}
}
