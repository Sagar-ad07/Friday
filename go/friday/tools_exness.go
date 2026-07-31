package friday

import (
	"context"
	"encoding/json"
	"fmt"
)

// ──────────────────────────────────────────────────────────────────────
// tools_exness.go — agent tools for the Exness personal MT5 account.
//
// The Exness account (login 167036042 @ Exness-MT5Real3) is wired to a
// SECOND gomt5.Client in the engine via a computed pipe name. Personal
// account, no $150 daily loss cap, no 5% drawdown cap, no profit target.
// Per user: run 24/7 under Friday supervision.
//
// These tools let the agent query the account, place trades, pull
// history, and select symbols — all routed to /mt5/exness/* endpoints.
// ──────────────────────────────────────────────────────────────────────

// ── Exness Account Info Tool ──
type ExnessAccountInfoTool struct{}

func (t *ExnessAccountInfoTool) Name() string { return "exness_account_info" }

func (t *ExnessAccountInfoTool) Description() string {
	return "Query the live Exness personal MT5 account (login 167036042 @ Exness-MT5Real3, 200:1 leverage, AED currency, no $150 cap, no profit limit — user directive: 24/7 trading). Returns real balance/equity/margin/leverage. Use this before placing any trade on the Exness account."
}

func (t *ExnessAccountInfoTool) Schema() ToolSchema {
	return ToolSchema{Type: "object", Properties: map[string]PropertyDef{}}
}

func (t *ExnessAccountInfoTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	result, err := engineGet("/mt5/exness/account")
	if err != nil {
		return map[string]any{
			"connected": false,
			"error":      err.Error(),
			"hint":       "Is the Exness MT5 terminal (D:\\MetaTrader 5\\terminal64.exe) running? Friday connects to it via a deterministic pipe name.",
		}, nil
	}
	return result, nil
}

// ── Exness Positions Tool ──
type ExnessPositionsTool struct{}

func (t *ExnessPositionsTool) Name() string { return "exness_positions" }

func (t *ExnessPositionsTool) Description() string {
	return "Get live open positions on the Exness personal MT5 account. Returns ticket/symbol/type/volume/prices/SL/TP/profit for each open trade. Use exness_account_info together with this when reporting the Exness account state to the user."
}

func (t *ExnessPositionsTool) Schema() ToolSchema {
	return ToolSchema{Type: "object", Properties: map[string]PropertyDef{}}
}

func (t *ExnessPositionsTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	result, err := engineGet("/mt5/exness/positions")
	if err != nil {
		return map[string]any{"connected": false, "error": err.Error()}, nil
	}
	return result, nil
}

// ── Execute Trade Exness Tool ──
type ExecuteTradeExnessTool struct{}

func (t *ExecuteTradeExnessTool) Name() string { return "execute_trade_exness" }

func (t *ExecuteTradeExnessTool) Description() string {
	return "Place a market trade on the Exness PERSONAL MT5 account (login 167036042 @ Exness-MT5Real3, 200:1 leverage, AED). NO $150 daily-loss cap, NO 5%% drawdown cap — 24/7 trading per user directive. Use exness_account_info first to check the account balance (currently ~14 AED = ~$3.80 USD — broker minimum lot is 0.01 = ~$1 margin). When placing: ALWAYS include sl and tp; EURUSD on Exness is EURUSDm (the 'm' suffix is what this broker streams — EURUSD plain does NOT tick here). Symbol field should be 'EURUSDm' or other 'm'-suffixed pairs."
}

func (t *ExecuteTradeExnessTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"symbol":    {Type: "string", Description: "Exness symbol (use 'm' suffix: EURUSDm, GBPUSDm, XAUUSDm, etc.)"},
			"type":      {Type: "string", Enum: []string{"buy", "sell"}, Description: "Trade direction (lowercase)"},
			"volume":    {Type: "number", Description: "Lot size (broker min 0.01). 0.01 lots of EURUSDm at 200:1 leverage = ~$0.07 margin."},
			"sl":        {Type: "number", Description: "Stop loss price (calculated from ATR or fixed pips)"},
			"tp":        {Type: "number", Description: "Take profit price (suggested 2x SL distance for 1:2 R:R)"},
		},
		Required: []string{"symbol", "type", "volume"},
	}
}

func (t *ExecuteTradeExnessTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Symbol string  `json:"symbol"`
		Type   string  `json:"type"`
		Volume float64 `json:"volume"`
		SL     float64 `json:"sl"`
		TP     float64 `json:"tp"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %v", err)
	}
	if p.Symbol == "" || p.Type == "" {
		return nil, fmt.Errorf("symbol and type required (symbol should be 'EURUSDm' not 'EURUSD' on Exness)")
	}
	// Safety: 14 AED (~$3.80 USD) account can't safely trade 0.10 lots —
	// broker min is 0.01. Cap at 0.05 to avoid immediate margin breach
	// even though there's no $150 cap on this personal account.
	if p.Volume <= 0 || p.Volume > 0.05 {
		return map[string]any{
			"success":    false,
			"error":      fmt.Sprintf("Volume %.2f is outside safe range (broker min 0.01, personal-cap-recommended max 0.05 for this 14 AED account)", p.Volume),
			"suggested":  0.01,
		}, nil
	}
	// Caps already happen in the engine handler; this guard is just for the
	// agent-facing message so failure isn't silent.
	body := map[string]any{
		"symbol": p.Symbol,
		"type":   p.Type,
		"volume": p.Volume,
		"sl":     p.SL,
		"tp":     p.TP,
	}
	result, err := enginePost("/mt5/exness/order", body)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	// Surface success + ticket so the agent can confirm to the user.
	return map[string]any{
		"success":      true,
		"account":     "exness",
		"result":       result,
		"note":         "Personal account — no $150 cap, 24/7 operation per user directive. Position monitor will log realized PnL when this ticket closes.",
	}, nil
}

// ── Exness Trade History Tool ──
type ExnessTradeHistoryTool struct{}

func (t *ExnessTradeHistoryTool) Name() string { return "exness_trade_history" }

func (t *ExnessTradeHistoryTool) Description() string {
	return "Pull closed-deal history for the Exness personal MT5 account (last N hours, default 24, max 168). Same code path the engine's Exness position monitor uses. Returns each closed deal's ticket/profit/swap/commission plus totals. Use this to answer 'what closed today on Exness' or 'how has the personal account done this week'."
}

func (t *ExnessTradeHistoryTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"hours": {Type: "number", Description: "Hours of history (default 24, max 168)", Default: 24},
		},
		Required: []string{},
	}
}

func (t *ExnessTradeHistoryTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct{ Hours int `json:"hours"` }
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %v", err)
	}
	if p.Hours <= 0 || p.Hours > 168 {
		p.Hours = 24
	}
	result, err := engineGet(fmt.Sprintf("/mt5/exness/history/%d", p.Hours))
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return result, nil
}

// ── Exness Bot Status Tool ──
//
// Mirrors the BG `trading_status` tool for the Exness autonomous bot.
// Same shape: running, in_trade, ticket, last_signal, last_error, wins,
// losses, daily_pnl etc. Plus a low_balance_notice flag the user wanted.

type ExnessBotStatusTool struct{}

func (t *ExnessBotStatusTool) Name() string { return "exness_bot_status" }

func (t *ExnessBotStatusTool) Description() string {
	return "Get the autonomous Exness TradingBot's status (running/in_trade/ticket/wins/losses/daily_pnl/last_signal/last_error/last_regime/last_adx/last_atr). last_adx > 20 means trending market. last_regime tells if trending or ranging. Same shape as trading_status for the BG bot. The Exness bot runs TPCS 24/7 with per-trade SL capped at 20 pips and ADX>20 trend filter."
}

func (t *ExnessBotStatusTool) Schema() ToolSchema {
	return ToolSchema{Type: "object", Properties: map[string]PropertyDef{}}
}

func (t *ExnessBotStatusTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	result, err := engineGet("/trading/exness/status")
	if err != nil {
		return map[string]any{
			"running": false,
			"error":    err.Error(),
			"hint":     "Is the engine :8001 up and the Exness terminal (D:\\MetaTrader 5\\terminal64.exe) running?",
		}, nil
	}
	return result, nil
}

// ── Market Analysis Tool ──

type MarketAnalysisTool struct{}

func (t *MarketAnalysisTool) Name() string { return "market_analysis" }

func (t *MarketAnalysisTool) Description() string {
	return "Fetch live H1 market analysis from Exness: ADX (>20 = trending), ATR (volatility in price units), RSI, regime (trending/ranging/volatile), support/resistance levels. Returns current live data from the broker. Use this when the user asks 'how is the market?' / 'what is the trend?' / 'ADX and ATR values' / 'is it a good time to trade?'."
}

func (t *MarketAnalysisTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"symbol": {Type: "string", Description: "Symbol to analyze (e.g. EURUSDm). Defaults to EURUSDm."},
		},
	}
}

func (t *MarketAnalysisTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct{ Symbol string `json:"symbol"` }
	if len(args) > 0 && string(args) != "null" {
		_ = json.Unmarshal(args, &params)
	}
	if params.Symbol == "" {
		params.Symbol = "EURUSDm"
	}
	result, err := engineGet("/trading/exness/market-analysis?symbol=" + params.Symbol)
	if err != nil {
		return map[string]any{
			"error": err.Error(),
			"hint":  "Is the engine :8001 up and the Exness terminal (D:\\MetaTrader 5\\terminal64.exe) running?",
		}, nil
	}
	return result, nil
}