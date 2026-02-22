package host

import (
	"context"
	"sync"
)

// RenderCache defines the interface for caching rendered HTML pages.
type RenderCache interface {
	// Clear invalidates all entries in the cache.
	Clear(ctx context.Context) error
	// Get retrieves a cached render by its key.
	Get(ctx context.Context, key string) (string, bool, error)
	// Set stores a rendered page in the cache.
	Set(ctx context.Context, key string, value string) error
}

// InMemoryRenderCache is a simple in-memory implementation of RenderCache.
type InMemoryRenderCache struct {
	mu     sync.RWMutex
	values map[string]string
}

// NewInMemoryRenderCache creates a new InMemoryRenderCache.
func NewInMemoryRenderCache() *InMemoryRenderCache {
	return &InMemoryRenderCache{
		values: make(map[string]string),
	}
}

// Clear invalidates all entries in the cache.
func (c *InMemoryRenderCache) Clear(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values = make(map[string]string)
	return nil
}

// Get retrieves a cached render by its key.
func (c *InMemoryRenderCache) Get(ctx context.Context, key string) (string, bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val, ok := c.values[key]
	return val, ok, nil
}

// Set stores a rendered page in the cache.
func (c *InMemoryRenderCache) Set(ctx context.Context, key string, value string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] = value
	return nil
}
