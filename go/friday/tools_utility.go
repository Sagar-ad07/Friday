package friday

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────
// JSON Tool
// ──────────────────────────────────────────────────────────────────────

type JSONTool struct{}
func (t *JSONTool) Name() string { return "json_tool" }
func (t *JSONTool) Description() string { return "Validate, format, or minify JSON" }
func (t *JSONTool) Schema() ToolSchema {
	return ToolSchema{Type:"object", Properties:map[string]PropertyDef{
		"action": {Type:"string", Enum:[]string{"validate","format","minify"}, Description:"Action"},
		"input":  {Type:"string", Description:"JSON string to process"},
	}, Required:[]string{"action","input"}}
}
func (t *JSONTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Action string `json:"action"`
		Input  string `json:"input"`
	}
	if err := json.Unmarshal(args, &p); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }
	var data any
	if err := json.Unmarshal([]byte(p.Input), &data); err != nil {
		return map[string]any{"valid":false,"error":err.Error()}, nil
	}
	switch p.Action {
	case "validate":
		return map[string]any{"valid":true}, nil
	case "format":
		b, _ := json.MarshalIndent(data, "", "  ")
		return map[string]any{"result":string(b)}, nil
	case "minify":
		b, _ := json.Marshal(data)
		return map[string]any{"result":string(b)}, nil
	}
	return nil, nil
}

// ──────────────────────────────────────────────────────────────────────
// Hash Tool
// ──────────────────────────────────────────────────────────────────────

type HashTool struct{}
func (t *HashTool) Name() string { return "hash" }
func (t *HashTool) Description() string { return "Compute hash of a string: md5, sha1, sha256" }
func (t *HashTool) Schema() ToolSchema {
	return ToolSchema{Type:"object", Properties:map[string]PropertyDef{
		"text":   {Type:"string", Description:"Text to hash"},
		"algorithm": {Type:"string", Enum:[]string{"md5","sha1","sha256"}, Description:"Hash algorithm", Default:"sha256"},
	}, Required:[]string{"text"}}
}
func (t *HashTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Text      string `json:"text"`
		Algorithm string `json:"algorithm"`
	}
	if err := json.Unmarshal(args, &p); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }
	if p.Algorithm == "" { p.Algorithm = "sha256" }
	data := []byte(p.Text)
	var h string
	switch p.Algorithm {
	case "md5": h = fmt.Sprintf("%x", md5.Sum(data))
	case "sha1": h = fmt.Sprintf("%x", sha1.Sum(data))
	case "sha256": h = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	return map[string]any{"input":p.Text, "algorithm":p.Algorithm, "hash":h}, nil
}

// ──────────────────────────────────────────────────────────────────────
// Random Tool
// ──────────────────────────────────────────────────────────────────────

type RandomTool struct{}
func (t *RandomTool) Name() string { return "random" }
func (t *RandomTool) Description() string { return "Generate random numbers, UUIDs, or passwords" }
func (t *RandomTool) Schema() ToolSchema {
	return ToolSchema{Type:"object", Properties:map[string]PropertyDef{
		"type":   {Type:"string", Enum:[]string{"number","uuid","password"}, Description:"Type of random data"},
		"min":    {Type:"number", Description:"Min value (for number)", Default:0},
		"max":    {Type:"number", Description:"Max value (for number)", Default:100},
		"length": {Type:"number", Description:"Length (for password)", Default:16},
	}, Required:[]string{"type"}}
}
func (t *RandomTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Type   string  `json:"type"`
		Min    float64 `json:"min"`
		Max    float64 `json:"max"`
		Length int     `json:"length"`
	}
	if err := json.Unmarshal(args, &p); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }
	if p.Length == 0 { p.Length = 16 }
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	switch p.Type {
	case "number":
		val := rng.Float64()*(p.Max-p.Min) + p.Min
		return map[string]any{"result": val, "type": "number"}, nil
	case "uuid":
		u := [16]byte{}
		rng.Read(u[:])
		u[6] = (u[6] & 0x0f) | 0x40
		u[8] = (u[8] & 0x3f) | 0x80
		return map[string]any{"result": fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:]), "type": "uuid"}, nil
	case "password":
		chars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()-_=+"
		b := make([]byte, p.Length)
		for i := range b {
			b[i] = chars[rng.Intn(len(chars))]
		}
		return map[string]any{"result": string(b), "type": "password", "length": p.Length}, nil
	}
	return nil, fmt.Errorf("unknown type: %s", p.Type)
}

// ──────────────────────────────────────────────────────────────────────
// Encode/Decode Tool
// ──────────────────────────────────────────────────────────────────────

type EncodeTool struct{}
func (t *EncodeTool) Name() string { return "encode" }
func (t *EncodeTool) Description() string { return "Encode or decode text (base64, url)" }
func (t *EncodeTool) Schema() ToolSchema {
	return ToolSchema{Type:"object", Properties:map[string]PropertyDef{
		"action": {Type:"string", Enum:[]string{"base64_encode","base64_decode","url_encode","url_decode"}, Description:"Action"},
		"text":   {Type:"string", Description:"Text to process"},
	}, Required:[]string{"action","text"}}
}
func (t *EncodeTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct{ Action, Text string }
	if err := json.Unmarshal(args, &p); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }
	var result string
	switch p.Action {
	case "base64_encode":
		result = base64.StdEncoding.EncodeToString([]byte(p.Text))
	case "base64_decode":
		b, err := base64.StdEncoding.DecodeString(p.Text)
		if err != nil { return nil, err }
		result = string(b)
	case "url_encode":
		result = url.QueryEscape(p.Text)
	case "url_decode":
		r, err := url.QueryUnescape(p.Text)
		if err != nil { return nil, err }
		result = r
	}
	return map[string]any{"action": p.Action, "result": result}, nil
}

// ──────────────────────────────────────────────────────────────────────
// IP Info Tool
// ──────────────────────────────────────────────────────────────────────

type IPInfoTool struct{}
func (t *IPInfoTool) Name() string { return "ip_info" }
func (t *IPInfoTool) Description() string { return "Get public IP address and location info (free API, no key needed)" }
func (t *IPInfoTool) Schema() ToolSchema {
	return ToolSchema{Type:"object", Properties:map[string]PropertyDef{
		"ip": {Type:"string", Description:"IP to look up (default: your public IP)", Default:""},
	}, Required:[]string{}}
}
func (t *IPInfoTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct{ IP string }
	if err := json.Unmarshal(args, &p); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }
	url := "http://ip-api.com/json/"
	if p.IP != "" { url += p.IP }
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil { return nil, err }
	defer resp.Body.Close()
	var data map[string]any
	json.NewDecoder(resp.Body).Decode(&data)
	return data, nil
}

// ──────────────────────────────────────────────────────────────────────
// Weather Tool
// ──────────────────────────────────────────────────────────────────────

type WeatherTool struct{}
func (t *WeatherTool) Name() string { return "weather" }
func (t *WeatherTool) Description() string { return "Get current weather for a city (free, no key needed via wttr.in)" }
func (t *WeatherTool) Schema() ToolSchema {
	return ToolSchema{Type:"object", Properties:map[string]PropertyDef{
		"location": {Type:"string", Description:"City name or coordinates"},
	}, Required:[]string{"location"}}
}
func (t *WeatherTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct{ Location string }
	if err := json.Unmarshal(args, &p); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(fmt.Sprintf("https://wttr.in/%s?format=j1", url.PathEscape(p.Location)))
	if err != nil { return nil, err }
	defer resp.Body.Close()
	var raw map[string]any
	json.NewDecoder(resp.Body).Decode(&raw)

	// Flatten into clean summary
	out := map[string]any{"location": p.Location}
	if cc, _ := raw["current_condition"].([]any); len(cc) > 0 {
		if c, ok := cc[0].(map[string]any); ok {
			out["condition"] = c["weatherDesc"]
			if desc, _ := c["weatherDesc"].([]any); len(desc) > 0 {
				if d, ok := desc[0].(map[string]any); ok { out["condition"] = d["value"] }
			}
			out["temp_c"], _ = c["temp_C"].(string)
			out["feels_like_c"], _ = c["FeelsLikeC"].(string)
			out["humidity"], _ = c["humidity"].(string)
			out["wind_speed_kmh"], _ = c["windspeedKmph"].(string)
			out["wind_dir"], _ = c["winddir16Point"].(string)
			out["pressure"], _ = c["pressure"].(string)
			out["visibility_km"], _ = c["visibility"].(string)
			out["uv_index"], _ = c["uvIndex"].(string)
		}
	}
	if na, _ := raw["nearest_area"].([]any); len(na) > 0 {
		if a, ok := na[0].(map[string]any); ok {
			if name, _ := a["areaName"].([]any); len(name) > 0 {
				if n, ok := name[0].(map[string]any); ok { out["city"] = n["value"] }
			}
			if cnt, _ := a["country"].([]any); len(cnt) > 0 {
				if c, ok := cnt[0].(map[string]any); ok { out["country"] = c["value"] }
			}
		}
	}
	if w, _ := raw["weather"].([]any); len(w) > 0 {
		if d, ok := w[0].(map[string]any); ok {
			out["forecast_high_c"], _ = d["maxtempC"].(string)
			out["forecast_low_c"], _ = d["mintempC"].(string)
			if ast, _ := d["astronomy"].([]any); len(ast) > 0 {
				if s, ok := ast[0].(map[string]any); ok { out["sunrise"], _ = s["sunrise"].(string); out["sunset"], _ = s["sunset"].(string) }
			}
		}
	}
	return out, nil
}

// ──────────────────────────────────────────────────────────────────────
// Word Count Tool
// ──────────────────────────────────────────────────────────────────────

type WordCountTool struct{}
func (t *WordCountTool) Name() string { return "word_count" }
func (t *WordCountTool) Description() string { return "Count words, characters, lines, sentences in text" }
func (t *WordCountTool) Schema() ToolSchema {
	return ToolSchema{Type:"object", Properties:map[string]PropertyDef{
		"text": {Type:"string", Description:"Text to analyze"},
	}, Required:[]string{"text"}}
}
func (t *WordCountTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct{ Text string }
	if err := json.Unmarshal(args, &p); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }
	words := len(strings.Fields(p.Text))
	chars := len(p.Text)
	lines := strings.Count(p.Text, "\n") + 1
	sentences := len(strings.Split(p.Text, ".")) - 1
	if sentences < 0 { sentences = 0 }
	return map[string]any{
		"words": words, "characters": chars,
		"lines": lines, "sentences": sentences,
		"avg_word_length": float64(chars) / float64(max(1, words)),
	}, nil
}

// ──────────────────────────────────────────────────────────────────────
// Ping Tool
// ──────────────────────────────────────────────────────────────────────

type PingTool struct{}
func (t *PingTool) Name() string { return "ping" }
func (t *PingTool) Description() string { return "Check if a host is reachable via ICMP ping" }
func (t *PingTool) Schema() ToolSchema {
	return ToolSchema{Type:"object", Properties:map[string]PropertyDef{
		"host": {Type:"string", Description:"Hostname or IP to ping"},
		"count": {Type:"number", Description:"Number of pings", Default:3},
	}, Required:[]string{"host"}}
}
func (t *PingTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct{ Host string; Count int `json:"count"` }
	if err := json.Unmarshal(args, &p); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }
	if p.Count == 0 { p.Count = 3 }
	if p.Count > 10 { p.Count = 10 }
	cmd := exec.CommandContext(ctx, "ping", "-n", fmt.Sprintf("%d", p.Count), p.Host)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return map[string]any{"host": p.Host, "reachable": false, "output": string(out)}, nil
	}
	return map[string]any{"host": p.Host, "reachable": true, "output": string(out)}, nil
}

// ──────────────────────────────────────────────────────────────────────
// Call Help Tool
// ──────────────────────────────────────────────────────────────────────

type CallHelpTool struct{}
func (t *CallHelpTool) Name() string { return "call_help" }
func (t *CallHelpTool) Description() string { return "Escalate to human when a task is beyond AI capabilities" }
func (t *CallHelpTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"issue":    {Type: "string", Description: "What needs human help"},
			"priority": {Type: "string", Enum: []string{"low","medium","high"}, Description: "Urgency"},
		},
		Required: []string{"issue"},
	}
}
func (t *CallHelpTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Issue    string `json:"issue"`
		Priority string `json:"priority"`
	}
	if err := json.Unmarshal(args, &p); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }
	if p.Priority == "" { p.Priority = "medium" }

	// Append to help file
	helpDir := filepath.Join(ProjectRoot, "data")
	os.MkdirAll(helpDir, 0755)
	log := fmt.Sprintf("[%s] [%s] %s\n", time.Now().Format(time.RFC3339), p.Priority, p.Issue)
	f, _ := os.OpenFile(filepath.Join(helpDir, "help_requests.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if f != nil { f.WriteString(log); f.Close() }

	return map[string]any{
		"message":  "Help requested: " + p.Issue,
		"priority": p.Priority,
		"status":   "logged for human review",
		"log_file": "data/help_requests.log",
	}, nil
}
