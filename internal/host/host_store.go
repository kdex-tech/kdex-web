package host

import (
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
		log:      ctrl.Log.WithName("hostStore"),
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
		handler = NewHostHandler(name, s.log)
		s.handlers[name] = handler
		s.log.Info("adding new host", "host", name)
	} else {
		s.log.Info("updating existing host", "host", name)
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
