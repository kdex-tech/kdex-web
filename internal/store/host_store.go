package store

import (
	"sync"
)

type HostStore struct {
	mu    sync.RWMutex
	hosts map[string]*HostHandler
}

func NewHostStore() *HostStore {
	return &HostStore{
		hosts: make(map[string]*HostHandler),
	}
}

func (s *HostStore) Delete(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.hosts, name)
}

func (s *HostStore) Get(name string) (*HostHandler, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	host, ok := s.hosts[name]
	return host, ok
}

func (s *HostStore) List() []*HostHandler {
	s.mu.RLock()
	defer s.mu.RUnlock()
	hosts := []*HostHandler{}
	for _, host := range s.hosts {
		hosts = append(hosts, host)
	}
	return hosts
}

func (s *HostStore) Set(host *HostHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hosts[host.Host.Name] = host
}
