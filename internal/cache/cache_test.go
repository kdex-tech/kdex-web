package cache

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
)

func TestNewCacheManager(t *testing.T) {
	tests := []struct {
		name       string
		args       func(t *testing.T) (string, string, *time.Duration, error)
		assertions func(t *testing.T, got CacheManager, err error)
	}{
		{
			name: "memory",
			args: func(t *testing.T) (string, string, *time.Duration, error) {
				return "", "test", new(10 * time.Millisecond), nil
			},
			assertions: func(t *testing.T, got CacheManager, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, got)
			},
		},
		{
			name: "memory - no ttl",
			args: func(t *testing.T) (string, string, *time.Duration, error) {
				return "", "test", nil, nil
			},
			assertions: func(t *testing.T, got CacheManager, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, got)
			},
		},
		{
			name: "memory - get same cache",
			args: func(t *testing.T) (string, string, *time.Duration, error) {
				return "", "test", nil, nil
			},
			assertions: func(t *testing.T, got CacheManager, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, got)
				c := got.GetCache("test", CacheOptions{})
				c2 := got.GetCache("test", CacheOptions{})
				assert.Same(t, c, c2)
			},
		},
		{
			name: "valkey",
			args: func(t *testing.T) (string, string, *time.Duration, error) {
				s, err := miniredis.Run()
				if err != nil {
					return "", "", nil, err
				}
				t.Cleanup(s.Close)
				return s.Addr(), "test", new(10 * time.Millisecond), nil
			},
			assertions: func(t *testing.T, got CacheManager, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, got)
			},
		},
		{
			name: "valkey - no valkey",
			args: func(t *testing.T) (string, string, *time.Duration, error) {
				return "localhost:6379", "test", new(10 * time.Millisecond), nil
			},
			assertions: func(t *testing.T, got CacheManager, err error) {
				assert.Error(t, err)
				assert.Nil(t, got)
			},
		},
		{
			name: "valkey - get same cache",
			args: func(t *testing.T) (string, string, *time.Duration, error) {
				s, err := miniredis.Run()
				if err != nil {
					return "", "", nil, err
				}
				t.Cleanup(s.Close)
				return s.Addr(), "test", new(10 * time.Millisecond), nil
			},
			assertions: func(t *testing.T, got CacheManager, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, got)
				c := got.GetCache("test", CacheOptions{})
				c2 := got.GetCache("test", CacheOptions{})
				assert.Same(t, c, c2)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, host, ttl, err := tt.args(t)
			if err != nil {
				t.Skip(err)
			}
			got, gotErr := NewCacheManager(addr, host, ttl)
			tt.assertions(t, got, gotErr)
		})
	}
}
