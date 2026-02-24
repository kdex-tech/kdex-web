package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
)

func TestValkeyCacheManager_GetCache(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Error(err)
	}

	tests := []struct {
		name       string
		args       func(t *testing.T) (addr, host, class string, opts CacheOptions)
		assertions func(t *testing.T, got Cache, cacheManager CacheManager)
	}{
		{
			name: "valkey",
			args: func(t *testing.T) (string, string, string, CacheOptions) {
				return s.Addr(), "foo", "test", CacheOptions{}
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
			args: func(t *testing.T) (string, string, string, CacheOptions) {
				return s.Addr(), "foo", "test", CacheOptions{}
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
			args: func(t *testing.T) (string, string, string, CacheOptions) {
				return s.Addr(), "foo", "test", CacheOptions{}
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
				err := got.Set(ctx, "test", "test")
				assert.NoError(t, err)
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
			args: func(t *testing.T) (string, string, string, CacheOptions) {
				return s.Addr(), "foo", "test", CacheOptions{}
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
			args: func(t *testing.T) (string, string, string, CacheOptions) {
				return s.Addr(), "foo", "test", CacheOptions{Uncycled: true}
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
			args: func(t *testing.T) (string, string, string, CacheOptions) {
				return s.Addr(), "foo", "test", CacheOptions{Uncycled: true}
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
			args: func(t *testing.T) (string, string, string, CacheOptions) {
				return s.Addr(), "foo", "test", CacheOptions{TTL: new(100 * time.Millisecond)}
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
				got = cacheManager.GetCache("test", CacheOptions{TTL: new(10 * time.Millisecond)})
				assert.Equal(t, time.Duration(10*time.Millisecond), got.TTL())

				// preexisting items in the cache still have the old expiry based on previous TTL
				val, ok, isCurrent, err = got.Get(ctx, "test")
				assert.NoError(t, err)
				assert.True(t, ok)
				assert.True(t, isCurrent)
				assert.Equal(t, "test", val)

				// a new item will have the new expiry
				err = got.Set(ctx, "foo", "foo")
				assert.NoError(t, err)
				s.FastForward(20 * time.Millisecond)

				// new item should be expired
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
			attr, host, class, opts := tt.args(t)
			cacheManager, err := NewCacheManager(attr, host, new(100*time.Millisecond))
			assert.NoError(t, err)
			got := cacheManager.GetCache(class, opts)
			tt.assertions(t, got, cacheManager)
			t.Cleanup(func() {
				cacheManager.Cycle(0, true)
			})
		})
	}
}
