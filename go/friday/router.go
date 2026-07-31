package friday

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ModelProvider struct {
	Name     string
	URL      string // base URL like http://localhost:11434
	Model    string // model name to use
	Timeout  time.Duration
	IsOllama bool   // true = uses /api/chat, false = uses /v1/chat/completions
	APIKey   string // optional Bearer token for cloud APIs
}

type ModelRouter struct {
	providers []ModelProvider
	httpClient *http.Client
}

func NewModelRouter() *ModelRouter {
	providers := []ModelProvider{}

 	// Priority Order:
 	// 1. GitHub gpt-4o-mini — FREE, 150 req/day
 	// 2. z.ai GLM-4-32B-0414-128K — PAID (~$0.1/1M) - instant fallback
 	// 3. z.ai GLM-4.5-Flash — FREE tier - when GitHub limits finish

 	// Tier 1: GitHub free - PRIMARY
 	if key := os.Getenv("GITHUB_TOKEN"); key != "" {
 		providers = append(providers, ModelProvider{
 			Name:     "github-free",
 			URL:      "https://models.inference.ai.azure.com/chat/completions",
 			Model:    "gpt-4o-mini",
 			Timeout:  30 * time.Second,
 			IsOllama: false,
 			APIKey:   key,
 		})
 	}

 	// Tier 2: SambaNova llama-3.3-70b — the SAME model family served by
 	// Cerebras/OpenRouter, so a fallover between them never changes the
 	// model's behavior. Free tier, OpenAI-compatible.
 	if key := os.Getenv("SAMBANOVA_API_KEYS"); key != "" {
 		providers = append(providers, ModelProvider{
 			Name:     "sambanova-free",
 			URL:      "https://api.sambanova.ai/v1/chat/completions",
 			Model:    "Meta-Llama-3.3-70B-Instruct",
 			Timeout:  45 * time.Second,
 			IsOllama: false,
 			APIKey:   key,
 		})
 	}

 	// Tier 2: z.ai GLM-4-32B-0414-128K - PAID FALLBACK (instant)
 	if key := os.Getenv("ZHIPU_API_KEY"); key != "" {
 		zhipu32bModel := os.Getenv("ZHIPU_32B_MODEL")
 		if zhipu32bModel == "" {
 			zhipu32bModel = "glm-4-32b-0414-128k"
 		}
 		providers = append(providers, ModelProvider{
 			Name:     "zai-paid",
 			URL:      "https://api.z.ai/api/paas/v4/chat/completions",
 			Model:    zhipu32bModel,
 			Timeout:  45 * time.Second,
 			IsOllama: false,
 			APIKey:   key,
 		})
 	}

 	// Tier 3: z.ai GLM-4.5-Flash - FREE FALLBACK (after GitHub limit)
 	if key := os.Getenv("ZHIPU_API_KEY"); key != "" {
 		freeZaiModel := os.Getenv("ZHIPU_FREE_MODEL")
 		if freeZaiModel == "" {
 			freeZaiModel = "glm-4.5-flash"
 		}
 		providers = append(providers, ModelProvider{
 			Name:     "zai-free",
 			URL:      "https://api.z.ai/api/paas/v4/chat/completions",
 			Model:    freeZaiModel,
 			Timeout:  45 * time.Second,
 			IsOllama: false,
 			APIKey:   key,
 		})
 	}

 	// Tier 5: OpenRouter free — last resort. Free tier is rate-limited
 	// (429s under load) so it sits at the tail of the chain where it only
 	// gets hit when every other brain is down.
 	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
 		providers = append(providers, ModelProvider{
 			Name:     "openrouter-free",
 			URL:      "https://openrouter.ai/api/v1/chat/completions",
 			Model:    "google/gemma-4-31b-it:free",
 			Timeout:  45 * time.Second,
 			IsOllama: false,
 			APIKey:   key,
 		})
 	}

	if len(providers) == 0 {
		log.Println("[ROUTER] WARNING: no AI providers configured.")
	}

	log.Printf("[ROUTER] %d providers: %s", len(providers), providerNames(providers))
	return &ModelRouter{
		providers:  providers,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

func providerNames(providers []ModelProvider) string {
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = fmt.Sprintf("%s(%s)", p.Name, p.Model)
	}
	return strings.Join(names, " -> ")
}

// Providers returns the configured chain so callers can advertise what the
// brain can actually serve (used by /v1/models and status reporting).
func (r *ModelRouter) Providers() []ModelProvider {
	out := make([]ModelProvider, len(r.providers))
	copy(out, r.providers)
	return out
}

// providerAttempt POSTs body to one provider, retrying once on flaky
// outcomes. Free tiers (SambaNova, OpenRouter) intermittently answer with
// empty 200s or 429s; a single quick retry usually lands the request.
// Retried: network errors, 429, 5xx, empty 200 bodies. Never retried:
// 4xx (bad key, bad model) — those won't heal in a second.
func (r *ModelRouter) providerAttempt(ctx context.Context, p ModelProvider, body []byte) (int, []byte, error) {
	url := p.URL
	if p.IsOllama {
		url += "/api/chat"
	}

	for attempt := 0; attempt < 2; attempt++ {
		ctx2, cancel := context.WithTimeout(ctx, p.Timeout)
		httpReq, err := http.NewRequestWithContext(ctx2, "POST", url, bytes.NewReader(body))
		if err == nil {
			httpReq.Header.Set("Content-Type", "application/json")
			httpReq.Header.Set("Accept", "application/json")
			if p.APIKey != "" {
				httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
			}
		}
		resp, doErr := r.httpClient.Do(httpReq)
		cancel()
		if doErr != nil {
			if attempt == 0 {
				time.Sleep(300 * time.Millisecond)
				continue
			}
			return 0, nil, doErr
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if attempt == 0 && (resp.StatusCode == 429 || resp.StatusCode >= 500 || len(raw) == 0) {
			time.Sleep(time.Second)
			continue
		}
		return resp.StatusCode, raw, nil
	}
	return 0, nil, fmt.Errorf("retries exhausted")
}

func (r *ModelRouter) Chat(ctx context.Context, messages []Message) (*ChatCompletionResponse, error) {
	lastErr := "all providers failed"
	for _, p := range r.providers {
		req := ChatRequest{
			Model:    p.Model,
			Messages: messages,
			Stream:   false,
		}
		body, _ := json.Marshal(req)

		status, respBody, err := r.providerAttempt(ctx, p, body)
		if err != nil {
			log.Printf("router: %s unreachable (%v), trying next", p.Name, err)
			lastErr = fmt.Sprintf("%s: %v", p.Name, err)
			continue
		}
		if status != 200 {
			log.Printf("router: %s returned %d body=%.200s, trying next", p.Name, status, string(respBody))
			lastErr = fmt.Sprintf("%s: status %d", p.Name, status)
			continue
		}

		if p.IsOllama {
			// Parse Ollama response format -> ChatCompletionResponse
			var oResp struct {
				Model     string  `json:"model"`
				CreatedAt string  `json:"created_at"`
				Message   Message `json:"message"`
				Done      bool    `json:"done"`
			}
			if err := json.Unmarshal(respBody, &oResp); err != nil {
				log.Printf("router: %s parse error (%v), trying next", p.Name, err)
				lastErr = fmt.Sprintf("%s: parse %v", p.Name, err)
				continue
			}
			if oResp.Message.Content == "" {
				log.Printf("router: %s empty response, trying next", p.Name)
				lastErr = fmt.Sprintf("%s: empty", p.Name)
				continue
			}
			result := &ChatCompletionResponse{
				ID:      fmt.Sprintf("chatcmpl-%s", uuid.New().String()[:8]),
				Object:  "chat.completion",
				Created: time.Now().Unix(),
				Model:   p.Model,
				Choices: []Choice{{
					Index: 0,
					Message: Message{
						Role:    "assistant",
						Content: oResp.Message.Content,
					},
					FinishReason: "stop",
				}},
			}
			log.Printf("router: %s answered (%.200s)", p.Name, oResp.Message.Content)
			return result, nil
		}

		// Bridge /api format is already ChatCompletionResponse
		var result ChatCompletionResponse
		if err := json.Unmarshal(respBody, &result); err != nil {
			log.Printf("router: %s parse error (%v), trying next", p.Name, err)
			lastErr = fmt.Sprintf("%s: parse %v", p.Name, err)
			continue
		}
		if len(result.Choices) == 0 || result.Choices[0].Message.Content == "" {
			log.Printf("router: %s empty response, trying next", p.Name)
			lastErr = fmt.Sprintf("%s: empty", p.Name)
			continue
		}
		log.Printf("router: %s answered (%.200s)", p.Name, result.Choices[0].Message.Content)
		return &result, nil
	}

	return nil, fmt.Errorf("%s", lastErr)
}

// TaskType classifies what kind of work the model needs to do
type TaskType string

const (
	TaskChat       TaskType = "chat"        // General conversation
	TaskCode       TaskType = "code"        // Code generation/analysis
	TaskVision     TaskType = "vision"      // Image analysis
	TaskFast       TaskType = "fast"        // Quick simple responses
	TaskReasoning  TaskType = "reasoning"   // Complex multi-step reasoning
)

// taskModelPrefs maps task types to preferred model names (not provider names).
// Environment variables can override: FRIDAY_TASK_MODEL_CHAT, FRIDAY_TASK_MODEL_CODE, etc.
// This decouples task routing from provider naming so new providers are used
// automatically when they offer the preferred model.
var taskModelPrefs = map[TaskType]string{
	TaskChat:      "gpt-4o-mini",
	TaskCode:      "gpt-4o-mini",
	TaskVision:    "gpt-4o-mini",
	TaskFast:      "gpt-4o-mini",
	TaskReasoning: "gpt-4o-mini",
}

func init() {
	envMap := map[TaskType]string{
		TaskChat:      os.Getenv("FRIDAY_TASK_MODEL_CHAT"),
		TaskCode:      os.Getenv("FRIDAY_TASK_MODEL_CODE"),
		TaskVision:    os.Getenv("FRIDAY_TASK_MODEL_VISION"),
		TaskFast:      os.Getenv("FRIDAY_TASK_MODEL_FAST"),
		TaskReasoning: os.Getenv("FRIDAY_TASK_MODEL_REASONING"),
	}
	for task, model := range envMap {
		if model != "" {
			taskModelPrefs[task] = model
		}
	}
}

// RouteByTask returns the best provider for a given task type.
// It finds the first provider whose model matches the preferred model
// for the task type. If no match, falls back to the first working provider.
func (r *ModelRouter) RouteByTask(task TaskType) *ModelProvider {
	if len(r.providers) == 0 {
		return nil
	}

	preferredModel := taskModelPrefs[task]
	// Exact model match first
	for i := range r.providers {
		if r.providers[i].Model == preferredModel {
			return &r.providers[i]
		}
	}
	// Partial model name match (e.g., "gpt-4o" matches "gpt-4o-mini")
	for i := range r.providers {
		if strings.Contains(r.providers[i].Model, preferredModel) || strings.Contains(preferredModel, r.providers[i].Model) {
			return &r.providers[i]
		}
	}

	// Fallback to first provider
	return &r.providers[0]
}

// ChatWithTask routes to the best model for the task type
func (r *ModelRouter) ChatWithTask(ctx context.Context, messages []Message, task TaskType) (*ChatCompletionResponse, error) {
	provider := r.RouteByTask(task)
	if provider == nil {
		return r.Chat(ctx, messages)
	}

	log.Printf("[ROUTER] task=%s → provider=%s model=%s", task, provider.Name, provider.Model)

	// Try the preferred provider first, then fall back to all
	req := ChatRequest{
		Model:    provider.Model,
		Messages: messages,
		Stream:   false,
	}

	body, _ := json.Marshal(req)
	ctx2, cancel := context.WithTimeout(ctx, provider.Timeout)
	defer cancel()

	var httpReq *http.Request
	var err error

	if provider.IsOllama {
		var oReq struct {
			Model    string    `json:"model"`
			Messages []Message `json:"messages"`
			Stream   bool      `json:"stream"`
		}
		oReq.Model = provider.Model
		oReq.Messages = messages
		oReq.Stream = false
		body, _ = json.Marshal(oReq)
		httpReq, err = http.NewRequestWithContext(ctx2, "POST", provider.URL+"/api/chat", bytes.NewReader(body))
	} else {
		httpReq, err = http.NewRequestWithContext(ctx2, "POST", provider.URL, bytes.NewReader(body))
	}

	if err != nil {
		return r.Chat(ctx, messages) // fallback to sequential try-all
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if provider.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}

	resp, err := r.httpClient.Do(httpReq)
	if err != nil {
		log.Printf("[ROUTER] preferred provider %s failed (%v), falling back", provider.Name, err)
		return r.Chat(ctx, messages)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		log.Printf("[ROUTER] preferred provider %s returned %d, falling back", provider.Name, resp.StatusCode)
		return r.Chat(ctx, messages)
	}

	if provider.IsOllama {
		var oResp struct {
			Model     string  `json:"model"`
			CreatedAt string  `json:"created_at"`
			Message   Message `json:"message"`
			Done      bool    `json:"done"`
		}
		if err := json.Unmarshal(respBody, &oResp); err != nil || oResp.Message.Content == "" {
			return r.Chat(ctx, messages)
		}
		return &ChatCompletionResponse{
			ID:      fmt.Sprintf("chatcmpl-%s", uuid.New().String()[:8]),
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   provider.Model,
			Choices: []Choice{{
				Index: 0,
				Message: Message{
					Role:    "assistant",
					Content: oResp.Message.Content,
				},
				FinishReason: "stop",
			}},
		}, nil
	}

	var result ChatCompletionResponse
	if err := json.Unmarshal(respBody, &result); err != nil || len(result.Choices) == 0 {
		return r.Chat(ctx, messages)
	}
	return &result, nil
}

// ChatWithTools sends messages + tool definitions to the best available
// provider that supports function calling. Falls back through all providers.
// Ollama providers use /api/chat with tools field (0.3+), others use /v1/chat/completions.
func (r *ModelRouter) ChatWithTools(ctx context.Context, messages []Message, tools []ToolDef) (*ChatCompletionResponse, error) {
	lastErr := "all providers failed"
	for _, p := range r.providers {
		reqMap := map[string]any{
			"model":    p.Model,
			"messages": messages,
			"stream":   false,
		}
		if len(tools) > 0 {
			reqMap["tools"] = tools
			reqMap["tool_choice"] = "auto"
		}
		body, _ := json.Marshal(reqMap)

		status, respBody, err := r.providerAttempt(ctx, p, body)
		if err != nil {
			log.Printf("[ROUTER-TOOLS] %s unreachable (%v)", p.Name, err)
			lastErr = fmt.Sprintf("%s: %v", p.Name, err)
			continue
		}
		if status != 200 {
			log.Printf("[ROUTER-TOOLS] %s returned %d body=%.300s", p.Name, status, string(respBody))
			lastErr = fmt.Sprintf("%s: status %d", p.Name, status)
			continue
		}

		if p.IsOllama {
			var oResp struct {
				Message struct {
					Role      string     `json:"role"`
					Content   string     `json:"content"`
					ToolCalls []ToolCall `json:"tool_calls"`
				} `json:"message"`
			}
			if err := json.Unmarshal(respBody, &oResp); err != nil {
			log.Printf("[ROUTER-TOOLS] %s parse error (%v)", p.Name, err)
				continue
			}
			result := &ChatCompletionResponse{
				ID:      fmt.Sprintf("chatcmpl-%s", uuid.New().String()[:8]),
				Object:  "chat.completion",
				Created: time.Now().Unix(),
				Model:   p.Model,
				Choices: []Choice{{
					Index: 0,
					Message: Message{
						Role:      oResp.Message.Role,
						Content:   oResp.Message.Content,
						ToolCalls: oResp.Message.ToolCalls,
					},
					FinishReason: "stop",
				}},
			}
			log.Printf("[ROUTER-TOOLS] %s answered (content=%d tool_calls=%d)", p.Name, len(oResp.Message.Content), len(oResp.Message.ToolCalls))
			return result, nil
		}

		var result ChatCompletionResponse
		if err := json.Unmarshal(respBody, &result); err != nil {
			log.Printf("[ROUTER-TOOLS] %s parse error (%v), trying next", p.Name, err)
			continue
		}
		if len(result.Choices) == 0 {
			continue
		}
		log.Printf("[ROUTER-TOOLS] %s answered (content=%d tool_calls=%d)", p.Name, len(result.Choices[0].Message.Content), len(result.Choices[0].Message.ToolCalls))
		return &result, nil
	}

	return nil, fmt.Errorf("%s", lastErr)
}

// ClassifyTask determines the task type from the user's message
func ClassifyTask(input string) TaskType {
	lower := strings.ToLower(input)

	// Vision: image-related
	if strings.Contains(lower, "image") || strings.Contains(lower, "picture") ||
		strings.Contains(lower, "photo") || strings.Contains(lower, "analyze this") {
		return TaskVision
	}

	// Code: programming-related
	if strings.Contains(lower, "code") || strings.Contains(lower, "function") ||
		strings.Contains(lower, "script") || strings.Contains(lower, "debug") ||
		strings.Contains(lower, "python") || strings.Contains(lower, "program") {
		return TaskCode
	}

	// Fast: short simple questions
	if len(input) < 50 && (strings.Contains(lower, "time") ||
		strings.Contains(lower, "status") || strings.Contains(lower, "hello") ||
		strings.Contains(lower, "hi ") || lower == "hi" || lower == "hey") {
		return TaskFast
	}

	// Action verbs that need tools - never route locally
	actionVerbs := []string{"check ", "show ", "get ", "find ", "tell me about",
		"what is the", "how is the", "look ", "pull ", "run ", "start ", "stop ",
		"buy ", "sell ", "trade ", "generate ", "search ", "lookup "}
	for _, v := range actionVerbs {
		if strings.HasPrefix(lower, v) || strings.Contains(lower, " "+v) {
			return TaskReasoning
		}
	}

	// Reasoning: complex multi-step
	if strings.Contains(lower, "analyze") || strings.Contains(lower, "compare") ||
		strings.Contains(lower, "strategy") || strings.Contains(lower, "calculate") ||
		strings.Contains(lower, "plan") || strings.Contains(lower, "explain why") {
		return TaskReasoning
	}

	return TaskChat
}

// Health returns nil if any provider responds to a lightweight probe.
// Unlike SelfDiagnose it never sends a chat request — just an HTTP reach check.
func (r *ModelRouter) Health(ctx context.Context) error {
	lastErr := fmt.Errorf("no providers configured")
	for _, p := range r.providers {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		url := p.URL
		if p.IsOllama {
			url = strings.TrimSuffix(p.URL, "/api/chat") + "/api/tags"
		} else {
			url = strings.TrimSuffix(url, "/chat/completions") + "/models"
		}
		req, err := http.NewRequestWithContext(probeCtx, "GET", url, nil)
		if err == nil && p.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+p.APIKey)
		}
		resp, err := r.httpClient.Do(req)
		cancel()
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", p.Name, err)
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode < 500 {
			return nil
		}
		lastErr = fmt.Errorf("%s: status %d", p.Name, resp.StatusCode)
	}
	return lastErr
}

// SelfDiagnoseProviders is the package-level entry point for startup diagnostics.
func SelfDiagnoseProviders() []string {
	router := NewModelRouter()
	return router.SelfDiagnose()
}

// SelfDiagnose tests all configured providers and returns which ones are alive.
// Called at startup so Friday knows her own capabilities.
func (r *ModelRouter) SelfDiagnose() []string {
	var alive []string
	testMsg := []Message{{Role: "user", Content: "ping"}}
	for _, p := range r.providers {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, err := r.Chat(ctx, testMsg)
		cancel()
		if err != nil {
			log.Printf("[DIAG] %s: DEAD (%v)", p.Name, err)
			continue
		}
		log.Printf("[DIAG] %s: ALIVE", p.Name)
		alive = append(alive, p.Name)
	}
	if len(alive) == 0 {
		log.Printf("[DIAG] WARNING: No providers are alive! Friday cannot think.")
	} else {
		log.Printf("[DIAG] %d/%d providers alive: %s", len(alive), len(r.providers), strings.Join(alive, ", "))
	}
	return alive
}
