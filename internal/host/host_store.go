package host

import (
	"sync"

	"github.com/go-logr/logr"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
)

type HostStore struct {
	mu       sync.RWMutex
	handlers map[string]*HostHandler
}

func NewHostStore() *HostStore {
	return &HostStore{
		handlers: make(map[string]*HostHandler),
	}
}

func (s *HostStore) Delete(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.handlers, name)
}

func (s *HostStore) Get(name string) (*HostHandler, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	handler, ok := s.handlers[name]
	return handler, ok
}

func (s *HostStore) GetOrUpdate(
	h *kdexv1alpha1.KDexHostController,
	scriptLibrary *kdexv1alpha1.KDexScriptLibrary,
	theme *kdexv1alpha1.KDexTheme,
	log logr.Logger,
) *HostHandler {
	s.mu.Lock()
	defer s.mu.Unlock()
	handler, ok := s.handlers[h.Name]
	if !ok {
		handler = NewHostHandler(log)
		s.handlers[h.Name] = handler
		log.Info("adding new host", "host", h.Name)
	} else {
		log.Info("updating existing host", "host", h.Name)
	}
	handler.SetHost(h, scriptLibrary, theme)
	return handler
}

func (s *HostStore) List() []*HostHandler {
	s.mu.RLock()
	defer s.mu.RUnlock()
	handlers := []*HostHandler{}
	for _, handler := range s.handlers {
		handlers = append(handlers, handler)
	}
	return handlers
}
