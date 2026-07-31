package llm

import (
	"time"
)

// CompletionRequest represents a request to an LLM
type CompletionRequest struct {
	Model          string
	Messages       []*Message
	Tools          []*Tool
	ToolChoice     ToolChoice
	Temperature    float64
	TopP           float64
	MaxTokens      int
	Stream         bool
	Stop           []string
	PresencePenalty float64
	FrequencyPenalty float64
	Seed           int64
	User           string
	Metadata       map[string]any
}

func NewCompletionRequest(model string, messages []*Message) *CompletionRequest {
	return &CompletionRequest{
		Model:       model,
		Messages:    messages,
		Temperature: 0.7,
		TopP:        1.0,
		MaxTokens:   4096,
		Stream:      false,
	}
}

// CompletionResponse represents a response from an LLM
type CompletionResponse struct {
	ID           string
	Model        string
	Created      time.Time
	Choices      []CompletionChoice
	Usage        *Usage
	Latency      time.Duration
	Provider     string
	Error        string
	Metadata     map[string]any
}

type CompletionChoice struct {
	Index        int
	Message      *Message
	FinishReason FinishReason
	Delta        *Message // For streaming
}

type FinishReason string

const (
	FinishReasonStop       FinishReason = "stop"
	FinishReasonLength     FinishReason = "length"
	FinishReasonToolCalls  FinishReason = "tool_calls"
	FinishReasonContentFilter FinishReason = "content_filter"
	FinishReasonError      FinishReason = "error"
)

// Usage tracks token consumption
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CachedTokens     int
}

func (u *Usage) Add(other *Usage) {
	u.PromptTokens += other.PromptTokens
	u.CompletionTokens += other.CompletionTokens
	u.TotalTokens += other.TotalTokens
	u.CachedTokens += other.CachedTokens
}

// StreamChunk represents a chunk in a streaming response
type StreamChunk struct {
	ID           string
	Model        string
	Created      time.Time
	Choices      []CompletionChoice
	Usage        *Usage
	Error        string
}

// ModelInfo describes an LLM model
type ModelInfo struct {
	ID            string
	Name          string
	Provider      string
	ContextWindow int
	MaxOutput     int
	SupportsTools bool
	SupportsVision bool
	SupportsStreaming bool
	InputCostPer1M float64
	OutputCostPer1M float64
	Description   string
}

// ProviderInfo describes an LLM provider
type ProviderInfo struct {
	Name        string
	Endpoint    string
	Models      []ModelInfo
	RequiresKey bool
	RateLimits  RateLimits
}

type RateLimits struct {
	RequestsPerMinute int
	TokensPerMinute   int
	ConcurrentRequests int
}