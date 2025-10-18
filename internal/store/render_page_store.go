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
	pages := []kdexv1alpha1.MicroFrontEndRenderPage{}
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

func (s *RenderPageStore) BuildMenuEntries(
	entry *menu.MenuEntry,
	parent *kdexv1alpha1.MicroFrontEndRenderPage,
) {
	for _, item := range s.List() {
		if (parent == nil && item.Spec.ParentPageRef == nil) ||
			(parent != nil && item.Spec.ParentPageRef != nil &&
				parent.Name == item.Spec.ParentPageRef.Name) {

			if entry.Children == nil {
				entry.Children = &map[string]*menu.MenuEntry{}
			}

			label := item.Spec.PageComponents.Title

			menuEntry := menu.MenuEntry{
				Name: item.Name,
				Path: item.Spec.Path,
			}

			if item.Spec.NavigationHints != nil {
				menuEntry.Icon = item.Spec.NavigationHints.Icon
				menuEntry.Weight = item.Spec.NavigationHints.Weight
			}

			(*entry.Children)[label] = &menuEntry

			s.BuildMenuEntries(&menuEntry, &item)
		}
	}
}
