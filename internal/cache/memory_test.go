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
	c := mgr.GetCache("html")

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
