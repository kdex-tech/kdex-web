package cache

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type InMemoryCache struct {
	class             string
	currentGeneration int64
	host              string
	mu                sync.RWMutex
	prefix            string // e.g. "{host:class:generation}:"
	prevPrefix        string // e.g. "{host:class:generation-1}:"
	segments          map[int64]map[string]memoryCacheEntry
	ttl               time.Duration
	uncycled          bool
	updateChan        chan time.Duration
}

var _ Cache = (*InMemoryCache)(nil)

func (c *InMemoryCache) Class() string {
	return c.class
}

func (c *InMemoryCache) Generation() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentGeneration
}

func (c *InMemoryCache) Host() string {
	return c.host
}

func (c *InMemoryCache) TTL() time.Duration {
	return c.ttl
}

func (c *InMemoryCache) Uncycled() bool {
	return c.uncycled
}

func (c *InMemoryCache) Get(ctx context.Context, key string) (string, bool, bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// 1. Try Current Generation
	if seg, ok := c.segments[c.currentGeneration]; ok {
		if entry, found := seg[key]; found {
			// LAZY DELETION CHECK
			if time.Now().After(entry.expiry) {
				// Just pretend it's not found. The reaper will get it later.
				return "", false, true, nil
			}
			return entry.value, true, true, nil // Found in current version
		}
	}

	// 2. Try Previous Generation (Searching for any other segment)
	// In a two-generation system, there will only be one other key.
	for gen, seg := range c.segments {
		if gen == c.currentGeneration {
			continue
		}
		if entry, found := seg[key]; found {
			// LAZY DELETION CHECK
			if time.Now().After(entry.expiry) {
				// Just pretend it's not found. The reaper will get it later.
				return "", false, true, nil
			}
			return entry.value, true, false, nil // Found, but it's the old version
		}
	}

	return "", false, true, nil // Not found in either version
}

// Set stores a rendered page in the cache.
func (c *InMemoryCache) Set(ctx context.Context, key string, value string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.segments[c.currentGeneration] == nil {
		c.segments[c.currentGeneration] = make(map[string]memoryCacheEntry)
	}

	c.segments[c.currentGeneration][key] = memoryCacheEntry{
		expiry: time.Now().Add(c.ttl),
		value:  value,
	}
	return nil
}

func (c *InMemoryCache) reap() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for _, seg := range c.segments {
		for key, entry := range seg {
			if now.After(entry.expiry) {
				delete(seg, key)
			}
		}
	}
}

func (c *InMemoryCache) startReaper(interval time.Duration) {
	c.updateChan = make(chan time.Duration, 1)

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				c.reap()

			case newInterval := <-c.updateChan:
				ticker.Reset(newInterval)
				c.reap()
			}
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

	oldGen := m.currentGeneration
	m.currentGeneration = generation

	for _, cache := range m.caches {
		if mCache, ok := cache.(*InMemoryCache); ok {
			if mCache.uncycled && !force {
				continue
			}
			mCache.mu.Lock()
			mCache.currentGeneration = generation

			// Ensure the new generation map exists
			if mCache.segments[generation] == nil {
				mCache.segments[generation] = make(map[string]memoryCacheEntry)
			}

			// If forced, wipe all generations except the current one
			if force {
				for g := range mCache.segments {
					if g != generation {
						delete(mCache.segments, g)
					}
				}
			} else {
				// Standard cycle: delete anything older than the previous gen
				for g := range mCache.segments {
					if g != generation && g != oldGen {
						delete(mCache.segments, g)
					}
				}
			}
			mCache.mu.Unlock()
		}
	}
	return nil
}

func (m *InMemoryCacheManager) GetCache(class string, opts CacheOptions) Cache {
	m.mu.RLock()
	cache, ok := m.caches[class]
	m.mu.RUnlock()

	if ok {
		mCache := cache.(*InMemoryCache)
		mCache.mu.Lock()
		mCache.uncycled = opts.Uncycled
		var newTTL *time.Duration
		if opts.TTL != nil && mCache.ttl != *opts.TTL {
			newTTL = opts.TTL
			mCache.ttl = *newTTL
		}
		mCache.mu.Unlock()
		// Send to channel AFTER unlocking the mutex
		if newTTL != nil {
			select {
			case mCache.updateChan <- *newTTL:
			default:
				// If channel is full, the reaper is already processing
				// or about to process an update.
			}
		}
		return cache
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	ttl := m.ttl
	if opts.TTL != nil {
		ttl = *opts.TTL
	}
	cache = &InMemoryCache{
		class:             class,
		currentGeneration: m.currentGeneration,
		host:              m.host,
		uncycled:          opts.Uncycled,
		segments:          make(map[int64]map[string]memoryCacheEntry),
		prefix:            fmt.Sprintf("{%s:%s:%d}:", m.host, class, m.currentGeneration),
		ttl:               ttl,
	}
	go cache.(*InMemoryCache).startReaper(ttl)
	m.caches[class] = cache
	return cache
}

type memoryCacheEntry struct {
	expiry     time.Time
	generation int64
	value      string
}
