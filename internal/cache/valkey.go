package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/valkey-io/valkey-go"
)

type ValkeyCache struct {
	client     valkey.Client
	class      string
	mu         sync.RWMutex
	prefix     string // e.g. "{host:class:generation}:"
	prevPrefix string // e.g. "{host:class:generation-1}:"
	ttl        time.Duration
}

var _ Cache = (*ValkeyCache)(nil)

func (s *ValkeyCache) Get(ctx context.Context, key string) (string, bool, bool, error) {
	s.mu.RLock()
	curr := s.prefix
	prev := s.prevPrefix
	s.mu.RUnlock()

	// 1. Try Current Generation
	val, found, err := s.getValue(ctx, curr+key)
	if err != nil || found {
		return val, found, true, err // Found in current version
	}

	// 2. Try Previous Generation
	if prev != "" {
		val, found, err := s.getValue(ctx, prev+key)
		if found {
			return val, true, false, err // Found, but it's the old version
		}
	}

	return "", false, false, nil
}

func (s *ValkeyCache) Set(ctx context.Context, key string, value string) error {
	s.mu.RLock()
	prefix := s.prefix
	s.mu.RUnlock()
	cmd := s.client.B().Set().Key(prefix + key).Value(value).Ex(s.ttl).Build()
	return s.client.Do(ctx, cmd).Error()
}

func (s *ValkeyCache) getValue(ctx context.Context, fullKey string) (string, bool, error) {
	cmd := s.client.B().Get().Key(fullKey).Build()
	val, err := s.client.Do(ctx, cmd).ToString()
	if valkey.IsValkeyNil(err) {
		return "", false, nil
	}
	return val, true, err
}

type ValkeyCacheManager struct {
	caches            map[string]Cache
	client            valkey.Client
	currentGeneration int64
	host              string
	mu                sync.RWMutex
	ttl               time.Duration
}

var _ CacheManager = (*ValkeyCacheManager)(nil)

func (m *ValkeyCacheManager) Cycle(generation int64, force bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.currentGeneration = generation

	for _, cache := range m.caches {
		if vCache, ok := cache.(*ValkeyCache); ok {
			vCache.mu.Lock()
			if force {
				vCache.prevPrefix = ""
			} else {
				vCache.prevPrefix = vCache.prefix
			}
			vCache.prefix = fmt.Sprintf("{%s:%s:%d}:", m.host, vCache.class, generation)
			vCache.mu.Unlock()
		}
	}
	return nil
}

func (m *ValkeyCacheManager) GetCache(class string) Cache {
	m.mu.RLock()
	if cache, ok := m.caches[class]; ok {
		m.mu.RUnlock()
		return cache
	}
	m.mu.RUnlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	cache := &ValkeyCache{
		client: m.client,
		class:  class,
		prefix: fmt.Sprintf("{%s:%s:%d}:", m.host, class, m.currentGeneration),
		ttl:    m.ttl,
	}
	m.caches[class] = cache
	return cache
}
