package friday

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
)

// LLMClient connects to the LLM Bridge (port 9001) with Ollama fallback
type LLMClient struct {
	baseURL    string
	httpClient *http.Client
	cache      *SemanticCache
	router     *ModelRouter
	mu         sync.RWMutex
}

func NewLLMClient(baseURL string) *LLMClient {
	return &LLMClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		cache:  NewSemanticCache(2000, 30*time.Minute),
		router: NewModelRouter(),
	}
}

// Chat calls the router directly (router handles provider fallback internally)
func (c *LLMClient) Chat(ctx context.Context, messages []Message, role Role) (*ChatCompletionResponse, error) {
	req := ChatRequest{
		Model:    "gpt-4o-mini",
		Messages: messages,
	}

	cacheKey := c.cache.Key(&req)
	if cached, _, ok := c.cache.Get(cacheKey); ok {
		cached.ID = fmt.Sprintf("chatcmpl-%s", uuid.New().String()[:8])
		cached.Created = time.Now().Unix()
		return cached, nil
	}

	result, err := c.router.Chat(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("all models failed: %v", err)
	}

	c.cache.Set(cacheKey, result, "router")
	return result, nil
}

// ChatWithTools runs an OpenAI-style chat completion with native function-calling
// (the "tools" param). Used by the agentic loop. Cache is intentionally bypassed
// — agentic calls differ across runs (different tool results, different
// message histories) and the cache key doesn't include the tools field.
//
// Pass tools=nil to force a final synthesis call (no tool promotions will be
// offered to the model, so it must reply with content).
func (c *LLMClient) ChatWithTools(ctx context.Context, messages []Message, tools []ToolDef) (*ChatCompletionResponse, error) {
	return c.router.ChatWithTools(ctx, messages, tools)
}

// StreamChat for SSE streaming
func (c *LLMClient) StreamChat(ctx context.Context, messages []Message, role Role) (<-chan *ChatCompletionResponse, error) {
	req := ChatRequest{
		Model:    "friday",
		Messages: messages,
		Stream:   true,
	}

	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}

	ch := make(chan *ChatCompletionResponse, 10)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		decoder := json.NewDecoder(resp.Body)
		for {
			var chunk ChatCompletionResponse
			if err := decoder.Decode(&chunk); err != nil {
				if err == io.EOF {
					return
				}
				log.Printf("Stream decode error: %v", err)
				return
			}
			select {
			case ch <- &chunk:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

// ProviderNames lists the configured brain providers (for status reporting).
func (c *LLMClient) ProviderNames() []string {
	if c.router == nil {
		return nil
	}
	names := make([]string, 0, 4)
	for _, p := range c.router.Providers() {
		names = append(names, p.Name)
	}
	return names
}

// LLMProviderNames is the package-level shortcut used by status handlers and
// local chat replies when no LLMClient is handy.
func LLMProviderNames() []string {
	return NewLLMClient("").ProviderNames()
}

// Health check - FAST AND RELIABLE
func (c *LLMClient) Health(ctx context.Context) error {
	if c.router != nil {
		return c.router.Health(ctx)
	}
	return fmt.Errorf("no LLM provider available")
}

// Models list
func (c *LLMClient) Models(ctx context.Context) ([]ModelInfo, error) {
	httpReq, _ := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/v1/models", nil)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Data, nil
}