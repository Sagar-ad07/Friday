package llm

import (
	"context"
	"time"
)

// Provider is the interface all LLM providers must implement
type Provider interface {
	Name() string
	Models() []ModelInfo
	Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
	Stream(ctx context.Context, req *CompletionRequest) (StreamReader, error)
	ValidateConfig() error
	HealthCheck(ctx context.Context) error
}

// StreamReader reads streaming responses
type StreamReader interface {
	Next() (*StreamChunk, error)
	Close() error
}

// Router selects the best provider for a request
type Router interface {
	SelectProvider(ctx context.Context, req *CompletionRequest) (Provider, error)
	RecordSuccess(provider string, latency time.Duration)
	RecordFailure(provider string, err error)
	GetStats() map[string]ProviderStats
}

type ProviderStats struct {
	Requests       int64
	Failures       int64
	TotalLatency   time.Duration
	AvgLatency     time.Duration
	LastSuccess    time.Time
	LastFailure    time.Time
	CircuitOpen    bool
}

// EmbeddingProvider generates embeddings
type EmbeddingProvider interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	EmbedSingle(ctx context.Context, text string) ([]float32, error)
	Dimensions() int
	Model() string
}

// ProviderConfig holds provider-specific configuration
type ProviderConfig struct {
	APIKey      string
	BaseURL     string
	Model       string
	Timeout     time.Duration
	MaxRetries  int
	RateLimit   RateLimits
	Headers     map[string]string
	ExtraParams map[string]any
}