package host

import (
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
)

type HostStore struct {
	mu       sync.RWMutex
	handlers map[string]*HostHandler
	log      logr.Logger
}

func NewHostStore() *HostStore {
	return &HostStore{
		handlers: make(map[string]*HostHandler),
		log:      ctrl.Log.WithName("hosts"),
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

func (s *HostStore) GetOrUpdate(name string) *HostHandler {
	s.mu.Lock()
	defer s.mu.Unlock()
	handler, ok := s.handlers[name]
	if !ok {
		handler = NewHostHandler(name)
		s.handlers[name] = handler
		s.log.V(1).Info("adding new host", name, fmt.Sprintf("%v", handler))
	} else {
		s.log.V(1).Info("updating existing host", name, fmt.Sprintf("%v", handler))
	}
	return handler
}

func (s *HostStore) List() map[string]*HostHandler {
	s.mu.RLock()
	defer s.mu.RUnlock()
	handlers := make(map[string]*HostHandler)
	if len(s.handlers) == 0 {
		return handlers
	}
	for k, handler := range s.handlers {
		handlers[k] = handler
	}
	return handlers
}
