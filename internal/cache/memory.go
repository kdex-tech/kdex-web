package cache

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type memoryCacheEntry struct {
	expiry     time.Time
	generation int64
	value      string
}

type InMemoryCache struct {
	class      string
	mu         sync.RWMutex
	prefix     string // e.g. "{host:class:generation}:"
	prevPrefix string // e.g. "{host:class:generation-1}:"
	values     map[string]memoryCacheEntry
	ttl        time.Duration
}

var _ Cache = (*InMemoryCache)(nil)

func (c *InMemoryCache) Get(ctx context.Context, key string) (string, bool, bool, error) {
	c.mu.RLock()
	curr := c.prefix
	prev := c.prevPrefix
	c.mu.RUnlock()

	entry, found := c.values[curr+key]
	if found {
		return entry.value, found, true, nil // Found in current version
	}

	// 2. Try Previous Generation
	if prev != "" {
		entry, found := c.values[prev+key]
		if found {
			return entry.value, true, false, nil // Found, but it's the old version
		}
	}

	return "", false, true, nil // Not found in either version
}

// Set stores a rendered page in the cache.
func (c *InMemoryCache) Set(ctx context.Context, key string, value string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[c.prefix+key] = memoryCacheEntry{
		expiry: time.Now().Add(c.ttl),
		value:  value,
	}
	return nil
}

func (c *InMemoryCache) startReaper(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			c.mu.Lock()
			now := time.Now()
			for key, entry := range c.values {
				if now.Sub(entry.expiry) > c.ttl {
					delete(c.values, key)
				}
			}
			c.mu.Unlock()
		}
	}()
}

type InMemoryCacheManager struct {
	caches            map[string]Cache
	currentGeneration int64
	host              string
	mu                sync.RWMutex
	ttl               time.Duration
}

var _ CacheManager = (*InMemoryCacheManager)(nil)

func (m *InMemoryCacheManager) Cycle(generation int64, force bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.currentGeneration = generation

	for _, cache := range m.caches {
		if vCache, ok := cache.(*InMemoryCache); ok {
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

func (m *InMemoryCacheManager) GetCache(class string) Cache {
	m.mu.RLock()
	if cache, ok := m.caches[class]; ok {
		m.mu.RUnlock()
		return cache
	}
	m.mu.RUnlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	cache := &InMemoryCache{
		class:  class,
		values: make(map[string]memoryCacheEntry),
		prefix: fmt.Sprintf("{%s:%s:%d}:", m.host, class, m.currentGeneration),
		ttl:    m.ttl,
	}
	go cache.startReaper(m.ttl)
	m.caches[class] = cache
	return cache
}
