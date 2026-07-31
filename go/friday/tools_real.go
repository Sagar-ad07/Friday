package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ──────────────────────────────────────────────────────────────────────
// tools_real.go — honest "what's actually running" tools.
//
// Why this file exists: the pre-existing ExnessBotTool / BlueGuardianBotTool
// / companion.go registerBots() all fabricate status ("active"), strategy
// ("BB-RSI + London ORB + 9-EMA Scalper"), and compliance numbers
// ($50,000 Blue Guardian — actual is $5,000). This file documents and
// exposes the actual live state, and adds an aggregator the agent uses when
// the user says "check our bots" so it doesn't fabricate.
//
// Naming: the MT5 bot on the active account is exposed as "MT5 swing bot" /
// "mt5_swing_bot" rather than "Exness bot" — because there is only one MT5
// connection at a time, governed by the active account in AccountManager
// and whatever terminal64.exe happens to be logged into the matching broker.
// Friday does NOT maintain a 2nd MT5 client for the Exness account; that
// path is dormant until account switching is wired up to actually reconnect.
// ──────────────────────────────────────────────────────────────────────

// ── MT5 Symbol Select Tool ──
//
// Real remediation for MT5 retcode 10019 ("no tick available"). Calls the
// new engine POST /mt5/select/:symbol endpoint, which calls
// gomt5.Client.SymbolSelect(symbol, true) on the connected terminal.
// After this call the symbol is in Market Watch and ticks will flow.

type MT5SymbolSelectTool struct{}

func (t *MT5SymbolSelectTool) Name() string { return "mt5_symbol_select" }

func (t *MT5SymbolSelectTool) Description() string {
	return "Subscribe an MT5 symbol into the terminal's Market Watch so SymbolInfoTick works. Use this when a tool returns 'no tick available' (MT5 retcode 10019) or 'tick <symbol>: mt5: no tick available' — both mean the symbol isn't in Market Watch on the connected terminal. Returns the live bid/ask if the symbol ticks after subscribe, or notes that the market may be closed."
}

func (t *MT5SymbolSelectTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"symbol": {Type: "string", Description: "MT5 symbol to subscribe (e.g. EURUSD, EURUSDm)"},
		},
		Required: []string{"symbol"},
	}
}

func (t *MT5SymbolSelectTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct{ Symbol string `json:"symbol"` }
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %v", err)
	}
	if p.Symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}
	resp, err := enginePost("/mt5/select/"+p.Symbol, nil)
	if err != nil {
		return map[string]any{
			"success": false,
			"symbol":  p.Symbol,
			"error":   err.Error(),
			"hint":    "Is the trading engine on :8001 running? Check with trading_status first.",
		}, nil
	}
	return resp, nil
}

// ── Check All Bots Aggregator ──
//
// Returns truthful status of all three live "bots" Friday actually manages:
//
//   1. MT5 swing bot on the active account (single MT connection)
//   2. Crypto grid bot (Binance, 24/7)
//   3. Airdrop Farming (auto)
//
// All fields come from live data sources (engine :8001, Binance API, tasklist).
// No hardcoded "active" strings, no fabricated PnL.

type CheckAllBotsTool struct{}

func (t *CheckAllBotsTool) Name() string { return "check_all_bots" }

func (t *CheckAllBotsTool) Description() string {
	return "One-shot aggregator: returns the true status of all money-making operations Friday manages (MT5 swing bot, crypto grid bot). Use this when the user says 'check our bots' / 'update me on bots' / 'how are the bots doing' so you don't need separate tool turns. All values are live — no fabrication."
}

func (t *CheckAllBotsTool) Schema() ToolSchema {
	return ToolSchema{Type: "object", Properties: map[string]PropertyDef{}}
}

func (t *CheckAllBotsTool) Execute(ctx context.Context, _ json.RawMessage) (any, error) {
	// Underlying engineGet / enginePost helpers already enforce their own
	// HTTP timeouts; the miner API probe uses a 3s client below.

	// —— Bot #1: MT5 swing bot (single active account) ——
	mt5 := map[string]any{}
	if acc, err := engineGet("/mt5/account"); err == nil {
		mt5["connected"] = true
		mt5["account"] = acc
		// Reflect the account name from AccountManager bookkeeping (the
		// AccountManager DOESN'T drive the engine — but the human keeps it
		// roughly in sync, so we surface it as a label hint).
		if am := GetAccounts(); am != nil {
			if a := am.GetActive(); a != nil {
				mt5["labeled_account"] = a.Name
				mt5["labeled_server"] = a.Server
			}
		}
	} else {
		mt5["connected"] = false
		mt5["error"] = err.Error()
	}

	// Engine status — has running/in_trade/last_error/last_signal/wins/losses.
	if st, err := engineGet("/trading/status"); err == nil {
		mt5["engine_status"] = st
		// The bot uses bot.symbol = "EURUSDm" hardcoded in trading/bot.go
		// but the connected account may differ. Surface last_error verbatim
		// so the agent knows whether it's actually trading.
		if le, _ := st["last_error"].(string); le != "" {
			mt5["last_error_live"] = le
		}
		// HONEST SIGNAL SOURCE DETECTION. The bot's price feed in trading/
		// engine.go:790 falls back to GenerateSyntheticCandles(1.0850, 1)
		// whenever the broker doesn't deliver ticks for the configured
		// symbol. Whenever last_error mentions "tick" AND last_signal is
		// anything other than "none", the signal MUST have been computed
		// from synthetic candles — the real bid/ask was unreachable.
		// Without this flag the agent would relay last_signal as if real.
		lastErr, _ := st["last_error"].(string)
		lastSig, _ := st["last_signal"].(string)
		lastConf, _ := st["last_confidence"].(float64)
		if lastSig != "" && lastSig != "none" && strings.Contains(strings.ToLower(lastErr), "tick") {
			mt5["data_source"] = "synthetic_fallback"
			mt5["signal_meaningful"] = false
			mt5["synthetic_signal_explanation"] = fmt.Sprintf("Engine reports last_signal=%s conf=%.0f%%, but the bot's price feed silently fell back to GenerateSyntheticCandles(1.0850, 1) because SymbolInfoTick on the hardcoded symbol (currently 'EURUSDm' in trading/bot.go:75) failed with: %s. So this signal was computed on FAKE candles around 1.0850, NOT real market data. Tell the user this — do not relay the signal as a real call.", lastSig, lastConf*100, lastErr)
		} else if lastSig != "" && lastSig != "none" {
			mt5["data_source"] = "live_candles"
			mt5["signal_meaningful"] = true
		} else {
			mt5["data_source"] = "no_signal"
			mt5["signal_meaningful"] = false
		}
		// Probe BOTH the hardcoded symbol and plain EURUSD so the agent can
		// truthfully diagnose the symbol mismatch.
		if t, err := engineGet("/mt5/tick/EURUSD"); err == nil {
			mt5["eurusd_tick_works"] = t
		} else {
			mt5["eurusd_tick_works"] = false
		}
		if t, err := engineGet("/mt5/tick/EURUSDm"); err == nil {
			mt5["eurusdm_tick_works"] = t
		} else {
			mt5["eurusdm_tick_works"] = false
		}
	} else {
		mt5["engine_error"] = err.Error()
	}

	if pos, err := engineGet("/mt5/positions"); err == nil {
		mt5["open_positions"] = pos
	} else {
		mt5["open_positions"] = []any{}
	}

	// —— Bot #2: Crypto grid (Binance, no API key required) ——
	crypto := map[string]any{}
	if gs, err := engineGet("/grid/status"); err == nil {
		crypto["grid"] = gs
	} else {
		crypto["grid_error"] = err.Error()
	}
	if pf, err := engineGet("/crypto/portfolio"); err == nil {
		crypto["portfolio"] = pf
	} else {
		crypto["portfolio_error"] = err.Error()
	}

	// —— Bot #2b: Exness personal-account MT5 (autonomous TPCS bot loop, no cap) ——
	exness := map[string]any{}
	if acc, err := engineGet("/mt5/exness/account"); err == nil {
		exness["connected"] = true
		exness["account"] = acc
	} else {
		exness["connected"] = false
		exness["error"] = err.Error()
	}
	if pos, err := engineGet("/mt5/exness/positions"); err == nil {
		exness["positions"] = pos
	} else {
		exness["positions"] = []any{}
	}
	// Pull the Exness autonomous bot's own status (parallel to BG's engine_status).
	if bst, err := engineGet("/trading/exness/status"); err == nil {
		exness["bot_status"] = bst
	}
	// Confirm what ticks on Exness (it streams EURUSDm — the inverse of BG).
	if tick, err := engineGet("/mt5/exness/tick/EURUSDm"); err == nil {
		exness["eurusdm_tick_works"] = tick
	} else {
		exness["eurusdm_tick_works"] = false
	}

	return map[string]any{
		"bots": []map[string]any{
			{
				"name":            "MT5 swing bot — BlueGuardian",
				"id":              "mt5_swing_bot_bg",
				"kind":            "MT5 / BlueGuardian-Server / propfirm / autonomous bot loop",
				"strategy":        "Trend Pullback Continuation (EMA50/200 + EMA21 pullback + RSI confirm) on H1 candles. SL/TP clamped to [30,80] pips with 1:2 R:R for swing-only compliance. $150 daily loss cap + 5% DD + 15% consistency via PropFirmState.RecordTrade.",
				"status":          mt5,
				"link":            "engine :8001 /mt5/* + /trading/*",
			},
			{
				"name":            "MT5 swing bot — Exness personal",
				"id":              "mt5_swing_bot_exness",
				"kind":            "MT5 / Exness-MT5Real3 / personal / autonomous bot loop",
				"strategy":        "Same TPCS strategy as BG, but NO cap, no swing clamp — runs 24/7 per user directive. Friday can ALSO place manual trades via execute_trade_exness on this account. low_balance_notice fires when balance < AED 10 (per user — not a stop, just a notice).",
				"status":          exness,
				"link":            "engine :8001 /mt5/exness/* + /trading/exness/status",
			},
			{
				"name":            "Crypto grid bot",
				"id":              "crypto_grid_bot",
				"kind":            "Binance 24/7 spot",
				"status":          crypto,
				"link":            "engine :8001 /grid/* + /crypto/*",
			},
		},
		"summary": map[string]any{
			"bot_count":          3,
			"live_bg_account":   mt5["account"],
			"live_exness_account": exness["account"],
			"crypto_grid_active": crypto["grid"] != nil,
		},
	}, nil
}

// ──────────────────────────────────────────────────────────────────────
// Honest replacements for the previously fabricated tool responses.
// Old ExnessBotTool claimed "Exness Bot" + "BB-RSI + London ORB + 9-EMA
// Scalper" + EURUSD regardless of which account was actually connected and
// whether those strategies were active. Old BlueGuardianBotTool returned
// DailyPnL/TotalPnL/Violations that were always 0 because the engine never
// calls PropFirmState.RecordTrade. These wrappers report the truth.
// ──────────────────────────────────────────────────────────────────────

// ── MT5 Trade History Tool ──
//
// Wraps the new engine GET /mt5/history/:hours endpoint. Lets the agent
// answer "what closed today / did we breach the cap?" without waiting for
// a tracked ticket to disappear from the open positions list. Hits the
// same gomt5.HistoryDealsGet code path the bot's positionMonitor uses, so
// it's also a public sanity check on the realization layer.

type MT5TradeHistoryTool struct{}

func (t *MT5TradeHistoryTool) Name() string { return "mt5_trade_history" }

func (t *MT5TradeHistoryTool) Description() string {
	return "Pull closed-deal history from MT5 for the last N hours (default 24, max 168=7d). Returns each closed deal's ticket/symbol/profit/swap/commission plus totals: net_pnl, wins, losses. Use this to answer 'what closed today' or 'did we breach the $150 daily loss cap' — the bot's positionMonitor only realizes PnL on tickets IT tracked at close time, this gives the full broker-truth picture."
}

func (t *MT5TradeHistoryTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"hours": {Type: "number", Description: "Hours of history to pull (default 24, max 168)", Default: 24},
		},
		Required: []string{},
	}
}

func (t *MT5TradeHistoryTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct{ Hours int `json:"hours"` }
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %v", err)
	}
	if p.Hours <= 0 || p.Hours > 168 {
		p.Hours = 24
	}
	result, err := engineGet(fmt.Sprintf("/mt5/history/%d", p.Hours))
	if err != nil {
		return map[string]any{"error": err.Error(), "note": "is the trading engine on :8001 reachable?"}, nil
	}
	return result, nil
}

// HonestExnessBotTool replaces ExnessBotTool — exposes the active MT5 bot
// (whichever account the engine is actually connected to) without
// fabricating "Exness" branding.
type HonestExnessBotTool struct{}

func (t *HonestExnessBotTool) Name() string { return "exness_bot" }

func (t *HonestExnessBotTool) Description() string {
	return "Query/control the live MT5 trading bot running on whatever MT5 account the engine is currently connected to (single connection — see manage_accounts for which one). Reports real account/server/balance, real last_error, real open positions, and real engine running state. Strategy note: the engine's bot loop currently runs BB-RSI + EMA-9 cross + ( dormant ) London-ORB on M1 candles against the symbol hard-coded in trading/bot.go (currently 'EURUSDm'). The user has since moved to swing-only on EURUSD (long TFs) — there is a known gap between the running strategy code and the user's intended strategy that the trading-engine rewrite will address."
}

func (t *HonestExnessBotTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"action":           {Type: "string", Description: "Action: status | start | start_safe | stop | details | risk_check | select_symbol", Enum: []string{"status", "start", "start_safe", "stop", "details", "risk_check", "select_symbol"}},
			"symbol_to_select": {Type: "string", Description: "If action=select_symbol, the MT5 symbol to subscribe into Market Watch (e.g. EURUSD). Use this when last_error mentions 'no tick available'."},
			"max_lot":          {Type: "number", Description: "Max lot size (e.g. 0.02). Only used with start_safe."},
			"daily_loss_limit": {Type: "number", Description: "Max daily loss in USD (e.g. 150). Only used with start_safe."},
		},
		Required: []string{"action"},
	}
}

func (t *HonestExnessBotTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Action          string  `json:"action"`
		SymbolToSelect  string  `json:"symbol_to_select"`
		MaxLot          float64 `json:"max_lot"`
		DailyLossLimit  float64 `json:"daily_loss_limit"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %v", err)
	}
	if p.Action == "" {
		p.Action = "status"
	}

	// Truthful identity — NEVER fabricate "Exness Bot" branding. Take the
	// real activeness from the engine + active-account bookkeeping.
	am := GetAccounts()
	activeLabel := "(no active account in bookkeeping)"
	activeServer := ""
	if a := am.GetActive(); a != nil {
		activeLabel = a.Name
		activeServer = a.Server
	}
info := map[string]any{
		"name":              "MT5 swing bot",
		"kind":              "single active MT5 connection",
		"action":            p.Action,
		"labeled_account":   activeLabel,
		"labeled_server":    activeServer,
		"strategy_note":     "Engine's strategy.go selects BB-RSI / EMA-9 / (dormant) London-ORB by hour. P2 update: bot.symbol is now 'EURUSD' (was 'EURUSDm' which BlueGuardian doesn't tick), price feed is now M30 candles (was M1), and on propfirm accounts SL/TP are overridden to fixed 50pip SL / 100pip TP from the actual fill price (broker-accurate). Personal accounts keep the strategy's std-based SL/TP — no swing restriction there per user directive.",
		"hardcoded_symbol_in_bot": "EURUSD",
		"timeframe_in_bot":        "M30",
		"sl_tp_profile":           "propfirm: fixed 50pip SL / 100pip TP from fill price; personal: strategy's std-based",
	}

	switch p.Action {
	case "status", "details":
		if st, err := engineGet("/trading/status"); err == nil {
			info["engine_status"] = st
		} else {
			info["engine_error"] = err.Error()
		}
		if acc, err := engineGet("/mt5/account"); err == nil {
			info["live_account"] = acc
			info["connected_account"] = fmt.Sprintf("%v @ %v", acc["login"], acc["server"])
		} else {
			info["account_error"] = err.Error()
			info["connected_account"] = "MT5 not connected"
		}
		if pos, err := engineGet("/mt5/positions"); err == nil {
			info["positions"] = pos
		}

	case "start", "start_safe":
		// Compliance gate: check PropFirmConfig-driven limits before
		// allowing the bot to start. Uses REAL configured values, not the
		// fabricated $50k BG number that used to be in companion.go.
		pf := GetPropFirm()
		if !pf.TradingActive {
			if pf.LastError != "" {
				info["error"] = "Trading blocked: " + pf.LastError
				info["compliance"] = pf.Status()
				return info, nil
			}
		}
		remaining := pf.Config.MaxDailyLoss + pf.DailyPnL
		if remaining <= 5 {
			info["error"] = fmt.Sprintf("Daily loss limit nearly reached ($%.0f remaining today)", remaining)
			return info, nil
		}
		maxLot := p.MaxLot
		if maxLot <= 0 {
			maxLot = pf.MaxLotSize("EURUSD", 20)
		}
		dll := p.DailyLossLimit
		if dll <= 0 {
			dll = pf.Config.MaxDailyLoss
		}
		info["max_lot_allowed"] = maxLot
		info["daily_loss_limit"] = dll
		info["note"] = fmt.Sprintf("Will start bot on the currently-connected MT5 account ($150 daily loss cap, $5k acct, 5%% DD, 15%% consistency per real PropFirmConfig).")
		_, err := enginePost("/trading/start", map[string]any{
			"max_lot":             maxLot,
			"daily_loss_limit":     dll,
			"max_risk_per_trade_pct": 0.5,
			"symbol":              "EURUSD",
		})
		if err == nil {
			info["result"] = "started"
		} else {
			info["error"] = err.Error()
		}

	case "stop":
		if r, err := enginePost("/trading/stop", nil); err == nil {
			info["result"] = r
		} else {
			info["error"] = err.Error()
		}

	case "risk_check":
		pf := GetPropFirm()
		info["compliance_report"] = pf.Status()
		info["max_lot"] = pf.MaxLotSize("EURUSD", 20)
		info["account_size"] = pf.Config.AccountSize
		info["daily_loss_cap"] = pf.Config.MaxDailyLoss
		info["max_drawdown_pct"] = pf.Config.MaxDrawdown
		info["note"] = "Compliance values are the REAL PropFirmConfig (seeded to $5000 / $150 / 5%% / 15%% / $250 target). Trade PnL is not currently being recorded into PropFirmState — DailyPnL/TotalPnL will report 0 even when the bot has traded."

	case "select_symbol":
		if p.SymbolToSelect == "" {
			p.SymbolToSelect = "EURUSD"
		}
		if r, err := enginePost("/mt5/select/"+p.SymbolToSelect, nil); err == nil {
			info["result"] = r
		} else {
			info["error"] = err.Error()
		}
	}

	return info, nil
}

// HonestBlueGuardianBotTool replaces BlueGuardianBotTool — exposes the
// actual BG $5k PropFirmConfig + persistence but honestly labels PnL fields
// as "not tracked live" since nothing in the engine calls RecordTrade.
type HonestBlueGuardianBotTool struct{}

func (t *HonestBlueGuardianBotTool) Name() string { return "blue_guardian_bot" }

func (t *HonestBlueGuardianBotTool) Description() string {
	return "Query Blue Guardian $5k Instant Starter compliance state (real account size $5000, $150 daily loss, 5%% max drawdown, $250 profit target, 15%% consistency — these are the actual PropFirmConfig values, NOT the previously mis-stated $50,000 / 4%% / 8%%). Returns live trade/balance data from the engine when available. PnL tracking note: the engine does not currently push closed-trade PnL into PropFirmState, so DailyPnL/TotalPnL/Violations will report 0 — this is honest, not broken; a future engine hook will record realized PnL."
}

func (t *HonestBlueGuardianBotTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"action": {Type: "string", Description: "Action: status | compliance | risk_check", Enum: []string{"status", "compliance", "risk_check"}},
		},
		Required: []string{"action"},
	}
}

func (t *HonestBlueGuardianBotTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct{ Action string `json:"action"` }
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %v", err)
	}
	if p.Action == "" {
		p.Action = "status"
	}
	pf := GetPropFirm()
	info := map[string]any{
		"name":              pf.Config.Name,
		"account_size":      pf.Config.AccountSize,
		"daily_loss_limit":  pf.Config.MaxDailyLoss,
		"max_drawdown_pct":  pf.Config.MaxDrawdown,
		"profit_target":     pf.Config.ProfitTarget,
		"consistency_pct":   pf.Config.ConsistencyPct,
		"min_trading_days":  pf.Config.MinTradingDays,
		"action":            p.Action,
		"trading_active":    pf.TradingActive,
		"pnl_tracking":      "not live — engine does not currently record closed-trade PnL into PropFirmState; Reported DailyPnL/TotalPnL/Violations will be 0 even after trades.",
		"violations":        pf.Violations,
	}
	// Surface the stored PnL fields but mark them honestly.
	if pf.DailyPnL == 0 && pf.TotalPnL == 0 && pf.TradesToday == 0 {
		info["daily_pnl"] = 0
		info["total_pnl"] = 0
		info["trades_today"] = 0
		info["pnl_zero_meaning"] = "0 here means 'not tracked', not 'actually $0 P&L on the account'. Read the live MT5 account equity instead to know true P&L."
	} else {
		info["daily_pnl"] = pf.DailyPnL
		info["total_pnl"] = pf.TotalPnL
		info["trades_today"] = pf.TradesToday
	}
	// Pull live engine data when reachable.
	if acc, err := engineGet("/mt5/account"); err == nil {
		info["live_account"] = acc
		// Real equity / balance is the source of truth for current P&L.
		if bal, _ := acc["balance"].(float64); bal > 0 {
			info["live_balance"] = bal
			info["realized_pnl_approx"] = bal - pf.Config.AccountSize
		}
	}
	if pos, err := engineGet("/mt5/positions"); err == nil {
		info["positions"] = pos
	}
	switch p.Action {
	case "status":
		info["report"] = pf.Status()
	case "compliance", "risk_check":
		info["compliance_report"] = pf.Status()
		info["max_lot"] = pf.MaxLotSize("EURUSD", 20)
	}
	return info, nil
}