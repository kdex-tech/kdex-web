package cache

import (
	"context"
	"time"

	"github.com/kdex-tech/kdex-host/internal/utils"
	"github.com/valkey-io/valkey-go"
)

type Cache interface {
	// Get retrieves a cached render by its key.
	Get(ctx context.Context, key string) (string, bool, bool, error)
	// Set stores a rendered page in the cache.
	Set(ctx context.Context, key string, value string) error
}

type CacheManager interface {
	Cycle(generation int64, force bool) error
	GetCache(class string) Cache
}

func NewCacheManager(addr, host string, ttl *time.Duration) (CacheManager, error) {
	if ttl == nil {
		ttl = utils.Ptr(24 * time.Hour)
	}

	if addr == "" {
		return &InMemoryCacheManager{
			caches:            make(map[string]Cache),
			currentGeneration: 0,
			host:              host,
			ttl:               *ttl,
		}, nil
	}

	client, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{addr}})
	if err != nil {
		return nil, err
	}
	return &ValkeyCacheManager{
		caches:            make(map[string]Cache),
		client:            client,
		currentGeneration: 0,
		host:              host,
		ttl:               *ttl,
	}, nil
}
