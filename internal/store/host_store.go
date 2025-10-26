package store

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

func (s *HostStore) GetOrDefault(
	name string,
	stylesheet *kdexv1alpha1.MicroFrontEndStylesheet,
	log logr.Logger,
) *HostHandler {
	s.mu.RLock()
	defer s.mu.RUnlock()
	handler, ok := s.handlers[name]
	if !ok {
		handler = NewHostHandler(stylesheet, log)
		s.handlers[name] = handler
		log.Info("tracking new host")
	}
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
