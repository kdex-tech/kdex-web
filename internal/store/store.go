package store

import (
	"sync"

	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
)

type TrackedHost struct {
	Host        kdexv1alpha1.MicroFrontEndHost
	RenderPages *RenderPageStore
}

type HostStore struct {
	mu    sync.RWMutex
	hosts map[string]TrackedHost
}

func NewHostStore() *HostStore {
	return &HostStore{
		hosts: make(map[string]TrackedHost),
	}
}

func (s *HostStore) Delete(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.hosts, name)
}

func (s *HostStore) Get(name string) (TrackedHost, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	host, ok := s.hosts[name]
	return host, ok
}

func (s *HostStore) List() []TrackedHost {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var hosts []TrackedHost
	for _, host := range s.hosts {
		hosts = append(hosts, host)
	}
	return hosts
}

func (s *HostStore) Set(host TrackedHost) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hosts[host.Host.Name] = host
}

type RenderPageStore struct {
	mu    sync.RWMutex
	pages map[string]kdexv1alpha1.MicroFrontEndRenderPage
}

func NewRenderPageStore() *RenderPageStore {
	return &RenderPageStore{
		pages: make(map[string]kdexv1alpha1.MicroFrontEndRenderPage),
	}
}

func (s *RenderPageStore) Delete(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pages, name)
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
	defer s.mu.Unlock()
	s.pages[page.Name] = page
}
