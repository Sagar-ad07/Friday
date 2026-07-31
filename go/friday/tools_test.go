package friday

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ─── Tool Registry Tests ───

func TestToolRegistryRegisterAndGet(t *testing.T) {
	r := NewToolRegistry()
	r.Register(&TimeTool{})

	tool, ok := r.Get("get_time")
	if !ok {
		t.Fatal("expected to find get_time tool after registration")
	}
	if tool.Name() != "get_time" {
		t.Errorf("expected name get_time, got %s", tool.Name())
	}
}

func TestToolRegistryGetMissing(t *testing.T) {
	r := NewToolRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Fatal("expected not to find nonexistent tool")
	}
}

func TestToolRegistryExecuteMissing(t *testing.T) {
	r := NewToolRegistry()
	_, err := r.Execute(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Fatal("expected error executing nonexistent tool")
	}
}

func TestToolRegistrySchemas(t *testing.T) {
	r := NewToolRegistry()
	r.Register(&TimeTool{})
	r.Register(&CalcTool{})

	schemas := r.Schemas()
	if len(schemas) != 2 {
		t.Errorf("expected 2 schemas, got %d", len(schemas))
	}
	if _, ok := schemas["get_time"]; !ok {
		t.Error("expected get_time in schemas")
	}
	if _, ok := schemas["calc"]; !ok {
		t.Error("expected calc in schemas")
	}
}

func TestToolRegistryExecuteBatch(t *testing.T) {
	r := NewToolRegistry()
	r.Register(&TimeTool{})

	calls := []ToolCall{
		{Function: FunctionCall{Name: "get_time", Arguments: json.RawMessage(`{}`)}},
	}
	results := r.ExecuteBatch(context.Background(), calls)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Error != "" {
		t.Errorf("unexpected error: %s", results[0].Error)
	}
}

// ─── Calc Tool Tests ───

func TestCalcToolAddition(t *testing.T) {
	tool := &CalcTool{}
	args := json.RawMessage(`{"expression":"2+2"}`)
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val, ok := result.(float64)
	if !ok {
		t.Fatalf("expected float64 result, got %T", result)
	}
	if val != 4 {
		t.Errorf("expected result 4, got %v", val)
	}
}

func TestCalcToolDivisionByZero(t *testing.T) {
	tool := &CalcTool{}
	args := json.RawMessage(`{"expression":"1/0"}`)
	result, _ := tool.Execute(context.Background(), args)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected map result")
	}
	if _, hasError := m["error"]; !hasError {
		t.Error("expected error for division by zero")
	}
}

// ─── Time Tool Tests ───

func TestTimeTool(t *testing.T) {
	tool := &TimeTool{}
	args := json.RawMessage(`{"timezone":"UTC"}`)
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected map result")
	}
	if m["timezone"] != "UTC" {
		t.Errorf("expected timezone UTC, got %v", m["timezone"])
	}
}

// ─── Risk Calculator Tests ───

func TestCalculateRiskTool(t *testing.T) {
	tool := &CalculateRiskTool{}
	args := json.RawMessage(`{"account_balance":5000,"risk_percent":1,"stop_loss_pips":30,"symbol":"EURUSD"}`)
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected map result")
	}
	positionSize, ok := m["position_size"].(float64)
	if !ok {
		t.Fatal("expected position_size to be float64")
	}
	// riskAmount = 5000 * 1 / 100 = 50
	// positionSize = 50 / (30 * 10) = 0.1666...
	expected := 50.0 / (30.0 * 10.0)
	if positionSize < expected-0.001 || positionSize > expected+0.001 {
		t.Errorf("expected position_size ~%.4f, got %.4f", expected, positionSize)
	}
}

func TestCalculateRiskToolZeroBalance(t *testing.T) {
	tool := &CalculateRiskTool{}
	args := json.RawMessage(`{"account_balance":0,"risk_percent":1,"stop_loss_pips":30}`)
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for zero balance")
	}
}

func TestCalculateRiskToolZeroStopLoss(t *testing.T) {
	tool := &CalculateRiskTool{}
	args := json.RawMessage(`{"account_balance":5000,"risk_percent":1,"stop_loss_pips":0}`)
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for zero stop loss")
	}
}

// ─── Memory Type Tests ───

func TestMemoryTypeConstants(t *testing.T) {
	if MemWorking != "WORKING" {
		t.Errorf("expected WORKING, got %s", MemWorking)
	}
	if MemEpisodic != "EPISODIC" {
		t.Errorf("expected EPISODIC, got %s", MemEpisodic)
	}
	if MemSemantic != "SEMANTIC" {
		t.Errorf("expected SEMANTIC, got %s", MemSemantic)
	}
}

// ─── Context Manager Tests ───

func TestManagedContextShortHistory(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}
	msgs := ManagedContext(history, "You are Friday.")
	if len(msgs) != 3 { // system + 2 messages
		t.Errorf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Error("expected first message to be system")
	}
}

func TestManagedContextLongHistory(t *testing.T) {
	history := make([]Message, 20)
	for i := range history {
		if i%2 == 0 {
			history[i] = Message{Role: "user", Content: "question " + string(rune('a'+i))}
		} else {
			history[i] = Message{Role: "assistant", Content: "answer " + string(rune('a'+i))}
		}
	}
	msgs := ManagedContext(history, "You are Friday.")
	// Should be: system + summary + MaxRecentMessages(6)
	if len(msgs) > MaxRecentMessages+2 {
		t.Errorf("expected at most %d messages, got %d", MaxRecentMessages+2, len(msgs))
	}
}

// ─── Task Classification Tests ───

func TestClassifyTask(t *testing.T) {
	tests := []struct {
		input string
		want  TaskType
	}{
		{"hello", TaskFast},
		{"hi", TaskFast},
		{"hey", TaskFast},
		{"what time is it", TaskFast},
		{"write a python script to scrape a website", TaskCode},
		{"analyze this image for me", TaskVision},
		{"compare the trading strategies and explain why one is better", TaskReasoning},
		{"tell me about the weather today", TaskReasoning},
	}

	for _, tt := range tests {
		got := ClassifyTask(tt.input)
		if got != tt.want {
			t.Errorf("ClassifyTask(%q) = %s, want %s", tt.input, got, tt.want)
		}
	}
}

// ─── Tool Cache Tests ───

func TestToolCacheSetGet(t *testing.T) {
	toolName := "crypto_price"
	args := json.RawMessage(`{"symbol":"BTCUSDT"}`)

	// Should not be cached initially
	_, ok := getCachedResult(toolName, args)
	if ok {
		t.Fatal("expected no cached result initially")
	}

	// Set cache
	setCachedResult(toolName, args, map[string]any{"price": 50000})

	// Should be cached now
	data, ok := getCachedResult(toolName, args)
	if !ok {
		t.Fatal("expected cached result after set")
	}
	m, ok := data.(map[string]any)
	if !ok {
		t.Fatal("expected map from cache")
	}
	if m["price"] != 50000 {
		t.Errorf("expected price 50000, got %v", m["price"])
	}
}

func TestToolCacheNonCacheable(t *testing.T) {
	// Terminal tool should not be cached
	toolName := "run_terminal"
	args := json.RawMessage(`{"command":"echo hello"}`)

	setCachedResult(toolName, args, "result")
	_, ok := getCachedResult(toolName, args)
	if ok {
		t.Fatal("terminal tool should not be cached")
	}
}

// ─── Sandbox/ProjectRoot Tests ───

func TestSandboxDir(t *testing.T) {
	dir := SandboxDir()
	if dir == "" {
		t.Fatal("expected non-empty sandbox dir")
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatal("sandbox dir should exist after SandboxDir() call")
	}
}

func TestProjectRoot(t *testing.T) {
	if ProjectRoot == "" {
		t.Fatal("expected non-empty project root")
	}
	if _, err := os.Stat(filepath.Join(ProjectRoot, "go.mod")); err != nil {
		// go.mod might not be at project root, check for data dir
		if _, err := os.Stat(filepath.Join(ProjectRoot, "data")); err != nil {
			t.Logf("warning: project root may not be correct: %s", ProjectRoot)
		}
	}
}

// ─── Hash Tool Tests ───

func TestHashToolSHA256(t *testing.T) {
	tool := &HashTool{}
	args := json.RawMessage(`{"text":"hello","algorithm":"sha256"}`)
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected map result")
	}
	hash, ok := m["hash"].(string)
	if !ok || len(hash) != 64 {
		t.Errorf("expected 64-char SHA256 hash, got %v", m["hash"])
	}
}

// ─── JSON Tool Tests ───

func TestJSONToolValidate(t *testing.T) {
	tool := &JSONTool{}
	args := json.RawMessage(`{"action":"validate","input":"{\"key\":\"value\"}"}`)
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected map result")
	}
	if m["valid"] != true {
		t.Error("expected valid=true for valid JSON")
	}
}

func TestJSONToolValidateInvalid(t *testing.T) {
	tool := &JSONTool{}
	args := json.RawMessage(`{"action":"validate","input":"{bad json}"}`)
	result, _ := tool.Execute(context.Background(), args)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected map result")
	}
	if m["valid"] != false {
		t.Error("expected valid=false for invalid JSON")
	}
}

// ─── Encode Tool Tests ───

func TestEncodeToolBase64(t *testing.T) {
	tool := &EncodeTool{}
	args := json.RawMessage(`{"action":"base64_encode","text":"hello"}`)
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected map result")
	}
	if m["result"] != "aGVsbG8=" {
		t.Errorf("expected aGVsbG8=, got %v", m["result"])
	}
}

