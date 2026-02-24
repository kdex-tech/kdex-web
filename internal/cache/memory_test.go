package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCacheLifecycle(t *testing.T) {
	// 1. Setup
	ttl := 10 * time.Millisecond
	mgr, err := NewCacheManager("", "", &ttl)
	assert.NoError(t, err)
	c := mgr.GetCache("html", CacheOptions{})

	ctx := context.Background()

	// 2. Set Generation 1
	mgr.Cycle(1, false)
	c.Set(ctx, "page1", "content-v1")

	// 3. Update to Generation 2 (The Cycle)
	mgr.Cycle(2, false)

	// 4. Verify Fallback (N-1)
	val, ok, isCurrent, err := c.Get(ctx, "page1")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.False(t, isCurrent)
	assert.Equal(t, "content-v1", val)

	// Trigger migration as discussed
	c.Set(ctx, "page1", val)

	// 5. Test TTL
	time.Sleep(100 * time.Millisecond) // Wait for reaper
	_, ok, _, err = c.Get(ctx, "page1")
	assert.NoError(t, err)
	assert.False(t, ok)
}

func TestInMemoryCacheManager_GetCache(t *testing.T) {
	tests := []struct {
		name       string
		args       func(t *testing.T) (class string, opts CacheOptions)
		assertions func(t *testing.T, got Cache, cacheManager CacheManager)
	}{
		{
			name: "memory",
			args: func(t *testing.T) (string, CacheOptions) {
				return "test", CacheOptions{}
			},
			assertions: func(t *testing.T, got Cache, cacheManager CacheManager) {
				assert.NotNil(t, got)
				assert.Equal(t, "test", got.Class())
				assert.Equal(t, int64(0), got.Generation())
				assert.Equal(t, "foo", got.Host())
				assert.Equal(t, time.Duration(100*time.Millisecond), got.TTL())
				assert.False(t, got.Uncycled())
			},
		},
		{
			name: "cycle - generation updates",
			args: func(t *testing.T) (string, CacheOptions) {
				return "test", CacheOptions{}
			},
			assertions: func(t *testing.T, got Cache, cacheManager CacheManager) {
				assert.NotNil(t, got)
				assert.Equal(t, "test", got.Class())
				assert.Equal(t, int64(0), got.Generation())
				assert.Equal(t, "foo", got.Host())
				assert.Equal(t, time.Duration(100*time.Millisecond), got.TTL())
				assert.False(t, got.Uncycled())
				cacheManager.Cycle(1, false)
				assert.Equal(t, int64(1), got.Generation())
				cacheManager.Cycle(2, false)
				assert.Equal(t, int64(2), got.Generation())
			},
		},
		{
			name: "cycle - with fallback",
			args: func(t *testing.T) (string, CacheOptions) {
				return "test", CacheOptions{}
			},
			assertions: func(t *testing.T, got Cache, cacheManager CacheManager) {
				assert.NotNil(t, got)
				assert.Equal(t, "test", got.Class())
				assert.Equal(t, int64(0), got.Generation())
				assert.Equal(t, "foo", got.Host())
				assert.Equal(t, time.Duration(100*time.Millisecond), got.TTL())
				assert.False(t, got.Uncycled())

				ctx := context.Background()

				// Set an item in the cache
				got.Set(ctx, "test", "test")
				val, ok, isCurrent, err := got.Get(ctx, "test")
				assert.NoError(t, err)
				assert.True(t, ok)
				assert.True(t, isCurrent)
				assert.Equal(t, "test", val)

				// Cycle to generation 1 - we got the fallback v(N-1)
				cacheManager.Cycle(1, false)
				assert.Equal(t, int64(1), got.Generation())
				val, ok, isCurrent, err = got.Get(ctx, "test")
				assert.NoError(t, err)
				assert.True(t, ok)
				assert.False(t, isCurrent)
				assert.Equal(t, "test", val)

				// Cycle to generation 2
				cacheManager.Cycle(2, false)
				assert.Equal(t, int64(2), got.Generation())
				val, ok, isCurrent, err = got.Get(ctx, "test")
				assert.NoError(t, err)
				assert.False(t, ok)
				assert.True(t, isCurrent)
				assert.Equal(t, "", val)
			},
		},
		{
			name: "cycle - force - no fallback",
			args: func(t *testing.T) (string, CacheOptions) {
				return "test", CacheOptions{}
			},
			assertions: func(t *testing.T, got Cache, cacheManager CacheManager) {
				assert.NotNil(t, got)
				assert.Equal(t, "test", got.Class())
				assert.Equal(t, int64(0), got.Generation())
				assert.Equal(t, "foo", got.Host())
				assert.Equal(t, time.Duration(100*time.Millisecond), got.TTL())
				assert.False(t, got.Uncycled())

				ctx := context.Background()

				// Set an item in the cache
				got.Set(ctx, "test", "test")
				val, ok, isCurrent, err := got.Get(ctx, "test")
				assert.NoError(t, err)
				assert.True(t, ok)
				assert.True(t, isCurrent)
				assert.Equal(t, "test", val)

				// Cycle to generation 1 - force, no fallback
				cacheManager.Cycle(1, true)
				assert.Equal(t, int64(1), got.Generation())
				val, ok, isCurrent, err = got.Get(ctx, "test")
				assert.NoError(t, err)
				assert.False(t, ok)
				assert.True(t, isCurrent)
				assert.Equal(t, "", val)
			},
		},
		{
			name: "uncycled",
			args: func(t *testing.T) (string, CacheOptions) {
				return "test", CacheOptions{Uncycled: true}
			},
			assertions: func(t *testing.T, got Cache, cacheManager CacheManager) {
				assert.NotNil(t, got)
				assert.Equal(t, "test", got.Class())
				assert.Equal(t, int64(0), got.Generation())
				assert.Equal(t, "foo", got.Host())
				assert.Equal(t, time.Duration(100*time.Millisecond), got.TTL())
				assert.True(t, got.Uncycled())

				ctx := context.Background()

				// Set an item in the cache
				got.Set(ctx, "test", "test")
				val, ok, isCurrent, err := got.Get(ctx, "test")
				assert.NoError(t, err)
				assert.True(t, ok)
				assert.True(t, isCurrent)
				assert.Equal(t, "test", val)

				// Cycle an uncycled cache without force does not clear the cache
				cacheManager.Cycle(1, false)
				assert.Equal(t, int64(0), got.Generation())
				val, ok, isCurrent, err = got.Get(ctx, "test")
				assert.NoError(t, err)
				assert.True(t, ok)
				assert.True(t, isCurrent)
				assert.Equal(t, "test", val)
			},
		},
		{
			name: "uncycled - force",
			args: func(t *testing.T) (string, CacheOptions) {
				return "test", CacheOptions{Uncycled: true}
			},
			assertions: func(t *testing.T, got Cache, cacheManager CacheManager) {
				assert.NotNil(t, got)
				assert.Equal(t, "test", got.Class())
				assert.Equal(t, int64(0), got.Generation())
				assert.Equal(t, "foo", got.Host())
				assert.Equal(t, time.Duration(100*time.Millisecond), got.TTL())
				assert.True(t, got.Uncycled())

				ctx := context.Background()

				// Set an item in the cache
				got.Set(ctx, "test", "test")
				val, ok, isCurrent, err := got.Get(ctx, "test")
				assert.NoError(t, err)
				assert.True(t, ok)
				assert.True(t, isCurrent)
				assert.Equal(t, "test", val)

				// Cycle with force clears an uncycled cache
				cacheManager.Cycle(1, true)
				assert.Equal(t, int64(1), got.Generation())
				val, ok, isCurrent, err = got.Get(ctx, "test")
				assert.NoError(t, err)
				assert.False(t, ok)
				assert.True(t, isCurrent)
				assert.Equal(t, "", val)
			},
		},
		{
			name: "update ttl",
			args: func(t *testing.T) (string, CacheOptions) {
				return "test", CacheOptions{TTL: new(100 * time.Millisecond)}
			},
			assertions: func(t *testing.T, got Cache, cacheManager CacheManager) {
				assert.NotNil(t, got)
				assert.Equal(t, "test", got.Class())
				assert.Equal(t, int64(0), got.Generation())
				assert.Equal(t, "foo", got.Host())
				assert.Equal(t, time.Duration(100*time.Millisecond), got.TTL())
				assert.False(t, got.Uncycled())

				ctx := context.Background()

				// Set an item in the cache
				got.Set(ctx, "test", "test")
				val, ok, isCurrent, err := got.Get(ctx, "test")
				assert.NoError(t, err)
				assert.True(t, ok)
				assert.True(t, isCurrent)
				assert.Equal(t, "test", val)

				// Update TTL
				got = cacheManager.GetCache("test", CacheOptions{TTL: new(1 * time.Millisecond)})
				assert.Equal(t, time.Duration(1*time.Millisecond), got.TTL())

				// preexisting items in the cache still have the old expiry based on previous TTL
				val, ok, isCurrent, err = got.Get(ctx, "test")
				assert.NoError(t, err)
				assert.True(t, ok)
				assert.True(t, isCurrent)
				assert.Equal(t, "test", val)

				// a new item will have the new expiry
				got.Set(ctx, "foo", "foo")
				time.Sleep(10 * time.Millisecond)

				// check it new item should be expired
				val, ok, isCurrent, err = got.Get(ctx, "foo")
				assert.NoError(t, err)
				assert.False(t, ok)
				assert.True(t, isCurrent)
				assert.Equal(t, "", val)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cacheManager, err := NewCacheManager("", "foo", new(100*time.Millisecond))
			assert.NoError(t, err)
			class, opts := tt.args(t)
			got := cacheManager.GetCache(class, opts)
			tt.assertions(t, got, cacheManager)
			t.Cleanup(func() {
				cacheManager.Cycle(0, true)
			})
		})
	}
}
