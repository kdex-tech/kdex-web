package store

import (
	"sync"
)

type HostStore struct {
	mu    sync.RWMutex
	hosts map[string]*TrackedHost
}

func NewHostStore() *HostStore {
	return &HostStore{
		hosts: make(map[string]*TrackedHost),
	}
}

func (s *HostStore) Delete(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.hosts, name)
}

func (s *HostStore) Get(name string) (*TrackedHost, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	host, ok := s.hosts[name]
	return host, ok
}

func (s *HostStore) List() []*TrackedHost {
	s.mu.RLock()
	defer s.mu.RUnlock()
	hosts := []*TrackedHost{}
	for _, host := range s.hosts {
		hosts = append(hosts, host)
	}
	return hosts
}

func (s *HostStore) Set(host *TrackedHost) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hosts[host.Host.Name] = host
}
