package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────
// Breach Tools — DeepSeek delegates to these when it needs vision
// or capabilities the primary model lacks
// ──────────────────────────────────────────────────────────────────────

// CallModelTool lets the primary model call any other model on demand.
// Format: {"prompt": "...", "model": "gemini-2.0-flash", "provider": "gemini"}
type CallModelTool struct{}

func (t *CallModelTool) Name() string        { return "call_model" }
func (t *CallModelTool) Description() string { return "Call another AI model for tasks the primary model cannot handle (vision, analysis, etc.). Set provider=gemini for free vision." }
func (t *CallModelTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"prompt":   {Type: "string", Description: "The prompt or task for the other model"},
			"model":    {Type: "string", Description: "Model ID (default: gemini-2.0-flash)"},
			"provider": {Type: "string", Description: "Provider: gemini, groq, openrouter (default: gemini)"},
		},
		Required: []string{"prompt"},
	}
}

func (t *CallModelTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Prompt   string `json:"prompt"`
		Model    string `json:"model"`
		Provider string `json:"provider"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}
	if params.Prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	if params.Model == "" {
		params.Model = "gemini-2.0-flash"
	}
	if params.Provider == "" {
		params.Provider = "gemini"
	}

	cfg := GetConfig()
	endpoint, ok := cfg.providerEndpoints[Provider(params.Provider)]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", params.Provider)
	}

	apiKey := cfg.Keys[string(Provider(params.Provider))]
	if apiKey == "" {
		return nil, fmt.Errorf("no API key for provider: %s (set %s env var)", params.Provider, endpoint.EnvKey)
	}

	// Build OpenAI-compatible request body
	body := map[string]interface{}{
		"model": params.Model,
		"messages": []map[string]interface{}{
			{"role": "user", "content": params.Prompt},
		},
		"max_tokens": 4096,
	}
	bodyJSON, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint.Endpoint+"/chat/completions", strings.NewReader(string(bodyJSON)))
	if err != nil {
		return nil, fmt.Errorf("breach request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	httpClient := &http.Client{Timeout: 60 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("breach call failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("breach returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("breach parse: %w", err)
	}

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("breach: no choices in response")
	}

	return map[string]interface{}{
		"response": result.Choices[0].Message.Content,
		"model":    params.Model,
		"provider": params.Provider,
	}, nil
}

// VisionAnalyzeTool specifically handles image analysis by delegating to Gemini.
type VisionAnalyzeTool struct{}

func (t *VisionAnalyzeTool) Name() string        { return "vision_analyze" }
func (t *VisionAnalyzeTool) Description() string { return "Analyze an image using a vision-capable model (Gemini). Send a base64 image or image URL." }
func (t *VisionAnalyzeTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"image": {Type: "string", Description: "Base64-encoded image data or image URL"},
			"question": {Type: "string", Description: "What to ask about the image"},
		},
		Required: []string{"image", "question"},
	}
}

func (t *VisionAnalyzeTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Image    string `json:"image"`
		Question string `json:"question"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	cfg := GetConfig()
	apiKey := cfg.Keys["gemini"]
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY not set — cannot analyze images. Get a free key at https://aistudio.google.com/apikey")
	}

	isURL := strings.HasPrefix(params.Image, "http://") || strings.HasPrefix(params.Image, "https://")

	var content []map[string]interface{}
	if isURL {
		content = []map[string]interface{}{
			{"type": "text", "text": params.Question},
			{"type": "image_url", "image_url": map[string]string{"url": params.Image}},
		}
	} else {
		content = []map[string]interface{}{
			{"type": "text", "text": params.Question},
			{"type": "image_url", "image_url": map[string]string{"url": "data:image/jpeg;base64," + params.Image}},
		}
	}

	body := map[string]interface{}{
		"model": "gemini-2.0-flash",
		"messages": []map[string]interface{}{
			{"role": "user", "content": content},
		},
		"max_tokens": 4096,
	}
	bodyJSON, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://generativelanguage.googleapis.com/v1/openai/chat/completions",
		strings.NewReader(string(bodyJSON)))
	if err != nil {
		return nil, fmt.Errorf("vision request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	httpClient := &http.Client{Timeout: 60 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vision call failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("vision returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("vision parse: %w", err)
	}

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("vision: no response")
	}

	return map[string]interface{}{
		"analysis": result.Choices[0].Message.Content,
		"model":    "gemini-2.0-flash",
	}, nil
}
