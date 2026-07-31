package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

type ToolSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]PropertyDef `json:"properties,omitempty"`
	Required   []string               `json:"required,omitempty"`
}

type PropertyDef struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Enum        []string `json:"enum,omitempty"`
	Default     any      `json:"default,omitempty"`
}

// Tool represents a callable tool
type Tool interface {
	Name() string
	Description() string
	Schema() ToolSchema
	Execute(ctx context.Context, args json.RawMessage) (any, error)
}

// ToolRegistry is the central tool registry
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]Tool)}
}

func (r *ToolRegistry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

func (r *ToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

func (r *ToolRegistry) Execute(ctx context.Context, name string, args json.RawMessage) (any, error) {
	r.mu.RLock()
	t, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("tool not found: %s", name)
	}
	return t.Execute(ctx, args)
}

func (r *ToolRegistry) ExecuteBatch(ctx context.Context, calls []ToolCall) []ToolResult {
	var wg sync.WaitGroup
	results := make([]ToolResult, len(calls))
	for i, call := range calls {
		wg.Add(1)
		go func(idx int, c ToolCall) {
			defer wg.Done()
			args := json.RawMessage(c.Function.Arguments)
			result, err := r.Execute(ctx, c.Function.Name, args)
			results[idx] = ToolResult{Tool: c.Function.Name, Result: result}
			if err != nil {
				results[idx].Error = err.Error()
			}
		}(i, call)
	}
	wg.Wait()
	return results
}

func (r *ToolRegistry) Schemas() map[string]ToolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()
	schemas := make(map[string]ToolSchema, len(r.tools))
	for name, t := range r.tools {
		schemas[name] = t.Schema()
	}
	return schemas
}

var GlobalRegistry = NewToolRegistry()

// DynamicTool wraps shell/Python commands as a Tool
type DynamicTool struct {
	name        string
	description string
	command     string // shell command (prefix "py:" for Python)
}

func (t *DynamicTool) Name() string        { return t.name }
func (t *DynamicTool) Description() string { return t.description }
func (t *DynamicTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"args": {Type: "string", Description: "Arguments to pass to the skill"},
		},
	}
}
func (t *DynamicTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct{ Args string `json:"args"` }
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %v", err)
	}

	if strings.HasPrefix(t.command, "py:") {
		pyCode := strings.TrimPrefix(t.command, "py:")
		cmd := exec.CommandContext(ctx, "python", "-c", pyCode)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("python execution failed: %w, output: %s", err, string(out))
		}
		return map[string]any{"output": string(out)}, nil
	}

	cmd := exec.CommandContext(ctx, "cmd", "/C", t.command)
	if params.Args != "" {
		cmd.Args = append(cmd.Args, params.Args)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("command failed: %w, output: %s", err, string(out))
	}
	return map[string]any{"output": string(out)}, nil
}

var dynamicToolsMu sync.RWMutex
var dynamicTools = make(map[string]*DynamicTool)

// RegisterSkillTool adds a new skill at runtime
func RegisterSkillTool(name, description, command string) string {
	if name == "" {
		return "error: name required"
	}
	if command == "" {
		return "error: command required"
	}

	dynamicToolsMu.Lock()
	defer dynamicToolsMu.Unlock()

	t := &DynamicTool{name: name, description: description, command: command}
	dynamicTools[name] = t
	GlobalRegistry.Register(t)
	return "Skill '" + name + "' registered: " + description
}

// SkillTool — lets the AI register new skills
type SkillTool struct{}

func (t *SkillTool) Name() string        { return "add_skill" }
func (t *SkillTool) Description() string { return "Register a new skill/tool at runtime. Provide name, description, and shell command or Python code (prefix with py:)." }
func (t *SkillTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"name":        {Type: "string", Description: "Skill name (lowercase, underscores)"},
			"description": {Type: "string", Description: "What this skill does"},
			"command":     {Type: "string", Description: "Shell command to run, or 'py:' prefix for Python code"},
		},
		Required: []string{"name", "description", "command"},
	}
}
func (t *SkillTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Command     string `json:"command"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}
	result := RegisterSkillTool(params.Name, params.Description, params.Command)
	return map[string]any{"result": result}, nil
}

// ListSkillsTool — lists all registered dynamic skills
type ListSkillsTool struct{}

func (t *ListSkillsTool) Name() string        { return "list_skills" }
func (t *ListSkillsTool) Description() string { return "List all registered skills" }
func (t *ListSkillsTool) Schema() ToolSchema {
	return ToolSchema{Type: "object", Properties: map[string]PropertyDef{}}
}
func (t *ListSkillsTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	dynamicToolsMu.RLock()
	defer dynamicToolsMu.RUnlock()
	skills := make([]map[string]string, 0)
	for _, dt := range dynamicTools {
		skills = append(skills, map[string]string{
			"name": dt.name, "description": dt.description, "command": dt.command,
		})
	}
	return map[string]any{"skills": skills, "count": len(skills)}, nil
}

func init() { RegisterAll() }

// RegisterAll registers all built-in tools (core tools only - trading proxied to :8001)
func RegisterAll() {
	GlobalRegistry.Register(&TerminalTool{})
	GlobalRegistry.Register(&CodeTool{})
	GlobalRegistry.Register(&FileTool{})
	GlobalRegistry.Register(&WebSearchTool{})
	GlobalRegistry.Register(&WebFetchTool{})
	GlobalRegistry.Register(&CalcTool{})
	GlobalRegistry.Register(&TimeTool{})
	GlobalRegistry.Register(&SystemInfoTool{})
	GlobalRegistry.Register(&TradingStartTool{})
	GlobalRegistry.Register(&TradingStopTool{})
	GlobalRegistry.Register(&TradingStatusTool{})
	GlobalRegistry.Register(&ExecuteTradeTool{})
	GlobalRegistry.Register(&GetMarketDataTool{})
	GlobalRegistry.Register(&GetAccountInfoTool{})
	GlobalRegistry.Register(&GetPositionsTool{})
	GlobalRegistry.Register(&GetOrdersTool{})
	GlobalRegistry.Register(&CalculateRiskTool{})
	GlobalRegistry.Register(&BacktestTool{})
	GlobalRegistry.Register(&CryptoPriceTool{})
	GlobalRegistry.Register(&CryptoPortfolioTool{})
	GlobalRegistry.Register(&CryptoGridTool{})
	GlobalRegistry.Register(&MarketRegimeTool{})
	GlobalRegistry.Register(&CryptoBacktestTool{})
	GlobalRegistry.Register(&CryptoTradeTool{})
	GlobalRegistry.Register(&MultiTFAnalysisTool{})
	GlobalRegistry.Register(&MomentumAnalysisTool{})
	GlobalRegistry.Register(&KellyPositionSizerTool{})
	GlobalRegistry.Register(&StrategyOptimizerTool{})
	GlobalRegistry.Register(&MemoryReadTool{})
	GlobalRegistry.Register(&MemoryWriteTool{})
	GlobalRegistry.Register(&CallModelTool{})
	GlobalRegistry.Register(&VisionAnalyzeTool{})
	GlobalRegistry.Register(&HonestExnessBotTool{})
	GlobalRegistry.Register(&HonestBlueGuardianBotTool{})
	GlobalRegistry.Register(&SkillTool{})
	GlobalRegistry.Register(&ListSkillsTool{})
	GlobalRegistry.Register(&ParallelSearchTool{})
	GlobalRegistry.Register(&CallHelpTool{})
	GlobalRegistry.Register(&WikipediaTool{})
	GlobalRegistry.Register(&BraveSearchTool{})
	GlobalRegistry.Register(&MojeekSearchTool{})
	GlobalRegistry.Register(&SearchAggregatorTool{})
	GlobalRegistry.Register(&JSONTool{})
	GlobalRegistry.Register(&HashTool{})
	GlobalRegistry.Register(&RandomTool{})
	GlobalRegistry.Register(&EncodeTool{})
	GlobalRegistry.Register(&IPInfoTool{})
	GlobalRegistry.Register(&WeatherTool{})
	GlobalRegistry.Register(&WordCountTool{})
	GlobalRegistry.Register(&PingTool{})
	GlobalRegistry.Register(&ManageAccountsTool{})
	GlobalRegistry.Register(&ExplainMT5ErrorTool{})
	GlobalRegistry.Register(&MT5SymbolSelectTool{})
	GlobalRegistry.Register(&CheckAllBotsTool{})
	GlobalRegistry.Register(&MT5TradeHistoryTool{})
	GlobalRegistry.Register(&ExnessAccountInfoTool{})
	GlobalRegistry.Register(&ExnessPositionsTool{})
	GlobalRegistry.Register(&ExecuteTradeExnessTool{})
	GlobalRegistry.Register(&ExnessTradeHistoryTool{})
	GlobalRegistry.Register(&ExnessBotStatusTool{})
	GlobalRegistry.Register(&MarketAnalysisTool{})
	GlobalRegistry.Register(&PassiveIncomeTool{})
	GlobalRegistry.Register(&AirdropHunterTool{})
	GlobalRegistry.Register(&ChainFarmTool{})
	GlobalRegistry.Register(&BridgeRegistryTool{})
	GlobalRegistry.Register(&BugBountyTool{})
	GlobalRegistry.Register(&ArbitrageMonitorTool{})
	GlobalRegistry.Register(&BrokenLinkTool{})
GlobalRegistry.Register(&SemanticRecallTool{})
	GlobalRegistry.Register(&FTS5RecallTool{})
	GlobalRegistry.Register(&GenerateStatementTool{})
	GlobalRegistry.Register(&TradeLedgerTool{})
	GlobalRegistry.Register(&ApproveStrategyTool{})

	// 0-Capital Earning Tools
	GlobalRegistry.Register(&AirdropFarmingTool{})
	GlobalRegistry.Register(&NodeRunnerTool{})
	GlobalRegistry.Register(&BandwidthTool{})

	// Testing Tools
	GlobalRegistry.Register(&TestingTool{})
}