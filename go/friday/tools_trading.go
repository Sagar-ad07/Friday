package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────
// Trading Tools (proxy to Go Trading Engine on :8001)
// ──────────────────────────────────────────────────────────────────────

type TradingStartTool struct{}

func (t *TradingStartTool) Name() string        { return "trading_start" }
func (t *TradingStartTool) Description() string { return "Start the automated trading bot" }
func (t *TradingStartTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{},
		Required: []string{},
	}
}
func (t *TradingStartTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	// Check if engine already running
	if resp, err := (&http.Client{Timeout: 2 * time.Second}).Get(engineBase + "/health"); err == nil {
		resp.Body.Close()
		return enginePost("/trading/start", map[string]string{})
	}

	// Start the engine process
	enginePath := filepath.Join(ProjectRoot, "trading_engine.exe")
	cmd := exec.Command(enginePath)
	if err := cmd.Start(); err != nil {
		return map[string]any{"error": "failed to start trading engine: " + err.Error()}, nil
	}

	// Wait for it to be ready
	for i := 0; i < 10; i++ {
		time.Sleep(500 * time.Millisecond)
		if resp, err := (&http.Client{Timeout: 1 * time.Second}).Get(engineBase + "/health"); err == nil {
			resp.Body.Close()
			return enginePost("/trading/start", map[string]string{})
		}
	}
	return map[string]any{"status": "engine started, awaiting ready"}, nil
}

type TradingStopTool struct{}
func (t *TradingStopTool) Name() string        { return "trading_stop" }
func (t *TradingStopTool) Description() string { return "Stop the automated trading bot" }
func (t *TradingStopTool) Schema() ToolSchema {
	return ToolSchema{Type: "object", Properties: map[string]PropertyDef{}}
}
func (t *TradingStopTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	// Try graceful stop via API
	if result, err := enginePost("/trading/stop", map[string]string{}); err == nil {
		return result, nil
	}
	// Force kill if API unreachable
	exec.Command("taskkill", "/f", "/im", "trading_engine.exe").Run()
	return map[string]any{"status": "stopped", "method": "force kill"}, nil
}

type TradingStatusTool struct{}

func (t *TradingStatusTool) Name() string        { return "trading_status" }
func (t *TradingStatusTool) Description() string { return "Get trading bot status" }
func (t *TradingStatusTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{},
		Required: []string{},
	}
}
func (t *TradingStatusTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	result, err := engineGet("/trading/status")
	if err != nil {
		return map[string]any{"running": false, "error": err.Error()}, nil
	}
	return result, nil
}

type ExecuteTradeTool struct{}

func (t *ExecuteTradeTool) Name() string        { return "execute_trade" }
func (t *ExecuteTradeTool) Description() string { return "Execute a trade order" }
func (t *ExecuteTradeTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"symbol":    {Type: "string", Description: "Trading symbol (e.g., EURUSD)"},
			"direction": {Type: "string", Enum: []string{"BUY", "SELL"}, Description: "Trade direction"},
			"size":      {Type: "number", Description: "Position size in lots"},
			"sl":        {Type: "number", Description: "Stop loss price"},
			"tp":        {Type: "number", Description: "Take profit price"},
		},
		Required: []string{"symbol", "direction", "size"},
	}
}
func (t *ExecuteTradeTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Symbol    string  `json:"symbol"`
		Direction string  `json:"direction"`
		Size      float64 `json:"size"`
		SL        float64 `json:"sl"`
		TP        float64 `json:"tp"`
	}
	if err := json.Unmarshal(args, &params); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }

	// Safety: verify which account is connected before trading
	acc, err := engineGet("/mt5/account")
	connectedAccount := "unknown"
	connectedBalance := 0.0
	if err == nil {
		if s, ok := acc["server"]; ok { connectedAccount = fmt.Sprintf("%v", s) }
		if b, ok := acc["balance"]; ok { connectedBalance, _ = b.(float64) }
	}

	// Max lot safety check
	if params.Size > 0.05 && connectedBalance < 10000 {
		return map[string]any{
			"success": false,
			"error":   fmt.Sprintf("Lot size %.2f too large for $%.0f account. Max: 0.05", params.Size, connectedBalance),
			"connected_account": connectedAccount,
		}, nil
	}
	if params.Size > 0.1 {
		return map[string]any{
			"success": false,
			"error":   "Lot size exceeds maximum allowed (0.1)",
		}, nil
	}

	// Translate the tool's external field names (direction/size, BUY/SELL) to
	// what the engine's /mt5/order handler expects (type/volume, buy/sell).
	dir := strings.ToLower(strings.TrimSpace(params.Direction))
	if dir != "buy" && dir != "sell" {
		return map[string]any{
			"success":           false,
			"error":             "direction must be BUY or SELL",
			"connected_account": connectedAccount,
		}, nil
	}
	engineReq := map[string]any{
		"symbol": params.Symbol,
		"volume": params.Size,
		"type":   dir, // lowercase to satisfy oneof=buy sell
		"sl":     params.SL,
		"tp":     params.TP,
	}
	result, err := enginePost("/mt5/order", engineReq)
	if err != nil {
		return map[string]any{
			"success": false,
			"error":   err.Error(),
			"connected_account": connectedAccount,
		}, nil
	}
	return map[string]any{
		"success": true,
		"result":  result,
		"connected_account": connectedAccount,
	}, nil
}

type GetMarketDataTool struct{}

func (t *GetMarketDataTool) Name() string        { return "get_market_data" }
func (t *GetMarketDataTool) Description() string { return "Get current market data for a symbol" }
func (t *GetMarketDataTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"symbol": {Type: "string", Description: "Trading symbol", Default: "EURUSD"},
		},
		Required: []string{},
	}
}
func (t *GetMarketDataTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Symbol string `json:"symbol"`
	}
	if err := json.Unmarshal(args, &params); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }
	if params.Symbol == "" {
		params.Symbol = "EURUSD"
	}
	result, err := engineGet("/mt5/tick/" + url.PathEscape(params.Symbol))
	if err != nil {
		return map[string]any{"symbol": params.Symbol, "error": err.Error()}, nil
	}
	return result, nil
}

type GetAccountInfoTool struct{}

func (t *GetAccountInfoTool) Name() string        { return "get_account_info" }
func (t *GetAccountInfoTool) Description() string { return "Get account information" }
func (t *GetAccountInfoTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{},
		Required: []string{},
	}
}
func (t *GetAccountInfoTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	result, err := engineGet("/mt5/account")
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return result, nil
}

type GetPositionsTool struct{}

func (t *GetPositionsTool) Name() string        { return "get_positions" }
func (t *GetPositionsTool) Description() string { return "Get open positions" }
func (t *GetPositionsTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{},
		Required: []string{},
	}
}
func (t *GetPositionsTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	result, err := engineGet("/mt5/positions")
	if err != nil {
		return []any{}, nil
	}
	return result, nil
}

type GetOrdersTool struct{}

func (t *GetOrdersTool) Name() string        { return "get_orders" }
func (t *GetOrdersTool) Description() string { return "Get pending orders" }
func (t *GetOrdersTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{},
		Required: []string{},
	}
}
func (t *GetOrdersTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	result, err := engineGet("/mt5/orders")
	if err != nil {
		return []any{}, nil
	}
	return result, nil
}

type CalculateRiskTool struct{}

func (t *CalculateRiskTool) Name() string        { return "calculate_risk" }
func (t *CalculateRiskTool) Description() string { return "Calculate position size based on risk" }
func (t *CalculateRiskTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"account_balance": {Type: "number", Description: "Account balance"},
			"risk_percent":    {Type: "number", Description: "Risk percentage (e.g., 2 for 2%)"},
			"stop_loss_pips":  {Type: "number", Description: "Stop loss in pips"},
			"symbol":          {Type: "string", Description: "Trading symbol"},
		},
		Required: []string{"account_balance", "risk_percent", "stop_loss_pips"},
	}
}
func (t *CalculateRiskTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		AccountBalance float64 `json:"account_balance"`
		RiskPercent    float64 `json:"risk_percent"`
		StopLossPips   float64 `json:"stop_loss_pips"`
		Symbol         string  `json:"symbol"`
	}
	if err := json.Unmarshal(args, &params); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }
	if params.AccountBalance <= 0 {
		return nil, fmt.Errorf("account_balance must be positive")
	}
	if params.RiskPercent <= 0 || params.RiskPercent > 100 {
		return nil, fmt.Errorf("risk_percent must be between 0 and 100")
	}
	if params.StopLossPips <= 0 {
		return nil, fmt.Errorf("stop_loss_pips must be positive")
	}
	riskAmount := params.AccountBalance * params.RiskPercent / 100
	pipValue := 10.0
	if params.Symbol == "EURUSD" {
		pipValue = 10.0
	}
	positionSize := riskAmount / (params.StopLossPips * pipValue)
	return map[string]any{
		"position_size": positionSize,
		"risk_amount":   riskAmount,
		"pip_value":     pipValue,
	}, nil
}

type BacktestTool struct{}

func (t *BacktestTool) Name() string        { return "backtest" }
func (t *BacktestTool) Description() string { return "Run backtest on historical data" }
func (t *BacktestTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"symbol":     {Type: "string", Description: "Trading symbol"},
			"start_date": {Type: "string", Description: "Start date (YYYY-MM-DD)"},
			"end_date":   {Type: "string", Description: "End date (YYYY-MM-DD)"},
			"sl_pips":    {Type: "number", Description: "Stop loss pips"},
			"tp_pips":    {Type: "number", Description: "Take profit pips"},
		},
		Required: []string{"symbol", "start_date", "end_date"},
	}
}
func (t *BacktestTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Symbol    string  `json:"symbol"`
		StartDate string  `json:"start_date"`
		EndDate   string  `json:"end_date"`
		SLPips    float64 `json:"sl_pips"`
		TPPips    float64 `json:"tp_pips"`
	}
	if err := json.Unmarshal(args, &params); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }
	result, err := enginePost("/backtest/run", params)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return result, nil
}

// ──────────────────────────────────────────────────────────────────────
// High-Probability Analysis Tools
// ──────────────────────────────────────────────────────────────────────

type MultiTFAnalysisTool struct{}

func (t *MultiTFAnalysisTool) Name() string        { return "multi_tf_analysis" }
func (t *MultiTFAnalysisTool) Description() string { return "Multi-timeframe consensus analysis — checks 15m, 1h, and 4h charts for aligned signals. Only recommends trade when ALL timeframes agree." }
func (t *MultiTFAnalysisTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"symbol": {Type: "string", Description: "Crypto symbol (default: BTCUSDT)"},
			"timeframes": {Type: "string", Description: "Comma-separated timeframes", Default: "15m,1h,4h"},
		},
		Required: []string{},
	}
}
func (t *MultiTFAnalysisTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct{ Symbol string `json:"symbol"` }
	if err := json.Unmarshal(args, &params); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }
	if params.Symbol == "" { params.Symbol = "BTCUSDT" }
	result, err := engineGet("/analysis/multitf/" + url.PathEscape(params.Symbol))
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return result, nil
}

type MomentumAnalysisTool struct{}
func (t *MomentumAnalysisTool) Name() string        { return "momentum_analysis" }
func (t *MomentumAnalysisTool) Description() string { return "Analyze momentum using MACD — tells you if the trend is strong or weak, and what direction" }
func (t *MomentumAnalysisTool) Schema() ToolSchema {
	return ToolSchema{Type:"object", Properties:map[string]PropertyDef{
		"symbol":   {Type:"string", Description:"Crypto symbol (default: BTCUSDT)"},
		"interval": {Type:"string", Description:"Timeframe", Default:"1h"},
	}, Required:[]string{}}
}
func (t *MomentumAnalysisTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct{ Symbol, Interval string }
	if err := json.Unmarshal(args, &params); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }
	if params.Symbol == "" { params.Symbol = "BTCUSDT" }
	if params.Interval == "" { params.Interval = "1h" }
	result, err := engineGet("/analysis/momentum/" + url.PathEscape(params.Symbol) + "/" + url.PathEscape(params.Interval))
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return result, nil
}

type KellyPositionSizerTool struct{}
func (t *KellyPositionSizerTool) Name() string        { return "kelly_sizer" }
func (t *KellyPositionSizerTool) Description() string { return "Calculate optimal position size using Kelly Criterion based on backtest performance" }
func (t *KellyPositionSizerTool) Schema() ToolSchema {
	return ToolSchema{Type:"object", Properties:map[string]PropertyDef{
		"symbol":   {Type:"string", Description:"Crypto symbol (default: BTCUSDT)"},
		"interval": {Type:"string", Description:"Timeframe", Default:"1h"},
	}, Required:[]string{}}
}
func (t *KellyPositionSizerTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct{ Symbol, Interval string }
	if err := json.Unmarshal(args, &params); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }
	if params.Symbol == "" { params.Symbol = "BTCUSDT" }
	if params.Interval == "" { params.Interval = "1h" }
	result, err := engineGet("/analysis/kelly/" + url.PathEscape(params.Symbol) + "/" + url.PathEscape(params.Interval))
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return result, nil
}

type StrategyOptimizerTool struct{}
func (t *StrategyOptimizerTool) Name() string        { return "strategy_optimizer" }
func (t *StrategyOptimizerTool) Description() string { return "Run the brute-force strategy optimizer to find best params for your symbol" }
func (t *StrategyOptimizerTool) Schema() ToolSchema {
	return ToolSchema{Type:"object", Properties:map[string]PropertyDef{
		"symbol": {Type:"string", Description:"Crypto symbol (default: BTCUSDT)"},
	}, Required:[]string{}}
}
func (t *StrategyOptimizerTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct{ Symbol string }
	if err := json.Unmarshal(args, &params); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }
	if params.Symbol == "" { params.Symbol = "BTCUSDT" }
	result, err := engineGet("/optimizer/quick/" + url.PathEscape(params.Symbol))
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return result, nil
}
