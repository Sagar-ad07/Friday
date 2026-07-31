package llm

import (
	"context"
	"time"
)

// CacheInterface defines the contract for semantic caching
type CacheInterface interface {
	Get(ctx context.Context, key string) (*CompletionResponse, bool)
	Set(ctx context.Context, key string, resp *CompletionResponse, ttl time.Duration)
	Delete(ctx context.Context, key string)
	Clear(ctx context.Context)
	Stats() CacheStats
}

type CacheStats struct {
	Hits   int64
	Misses int64
	Size   int64
}

// RouterInterface defines the contract for provider routing
type RouterInterface interface {
	Route(ctx context.Context, req *CompletionRequest) (Provider, *CompletionRequest, error)
	GetProvider(name string) (Provider, bool)
	ListProviders() []string
}