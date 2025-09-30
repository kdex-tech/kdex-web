package store

import (
	"sync"

	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
)

type TrackedHost struct {
	Host kdexv1alpha1.MicroFrontEndHost
}

type HostStore struct {
	mu    sync.RWMutex
	hosts map[string]TrackedHost
}

func New() *HostStore {
	return &HostStore{
		hosts: make(map[string]TrackedHost),
	}
}

func (s *HostStore) Set(host TrackedHost) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hosts[host.Host.Name] = host
}

func (s *HostStore) Get(name string) (TrackedHost, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	host, ok := s.hosts[name]
	return host, ok
}

func (s *HostStore) Delete(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.hosts, name)
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
