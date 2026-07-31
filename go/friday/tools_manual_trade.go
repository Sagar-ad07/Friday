package friday

import (
	"context"
	"encoding/json"
	"math"
)

// ──────────────────────────────────────────────────────────────────────
// Manual Trade Tool — "Friday, manual trade BUY EURUSDm 0.02"
//
// One chat/voice message => the model emits `manual_trade` => one order is
// placed. This is a HUMAN-INITIATED skill (the human types/speaks the order),
// never an autonomous timer/daemon. The order is routed through the engine's
// real /mt5/order (BlueGuardian prop, daily-cap + TP-clamp enforced) or
// /mt5/exness/order (personal, no cap) endpoints — so the prop firm sees a
// normal manual deal, and the cap guard still blocks a cap-breached entry.
// ──────────────────────────────────────────────────────────────────────

type ManualTradeTool struct{}

func (t *ManualTradeTool) Name() string { return "manual_trade" }

func (t *ManualTradeTool) Description() string {
	return "Place a single manual market order on the laptop MT5 terminal — the human decides symbol, lots, SL/TP, exactly like clicking buy/sell in the deepchart. Choose account 'primary' (BlueGuardian prop firm: daily $37.50 cap + TP clamp apply) or 'exness' (personal account, no cap, 24/7). If SL/TP are omitted, Friday derives them from the live tick (50-pip SL, 1:2 R:R TP) — review the proposal in the result before the human confirms the next one. Max 0.05 lots per order. Always include symbol/type/volume. Exness symbols need the 'm' suffix (EURUSDm)."
}

func (t *ManualTradeTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"account": {
				Type: "string", Enum: []string{"primary", "exness"},
				Description: "primary = BlueGuardian prop firm (cap applies), exness = personal account (no cap)",
				Default:     "primary",
			},
			"symbol": {Type: "string", Description: "Symbol, e.g. EURUSD, AUDJPY, EURUSDm (Exness uses 'm' suffix)"},
			"type":   {Type: "string", Enum: []string{"buy", "sell"}, Description: "buy or sell (lowercase)"},
			"volume": {Type: "number", Description: "Lot size 0.01–0.05"},
			"sl":     {Type: "number", Description: "Stop loss price (optional; derived from tick if omitted)"},
			"tp":     {Type: "number", Description: "Take profit price (optional; derived from tick if omitted)"},
		},
		Required: []string{"symbol", "type", "volume"},
	}
}

func (t *ManualTradeTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Account string  `json:"account"`
		Symbol  string  `json:"symbol"`
		Type    string  `json:"type"`
		Volume  float64 `json:"volume"`
		SL      float64 `json:"sl"`
		TP      float64 `json:"tp"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, errInvalidArgs("manual_trade: " + err.Error())
	}
	if p.Symbol == "" || p.Type == "" {
		return nil, errInvalidArgs("symbol and type are required (type: buy|sell)")
	}
	if p.Volume <= 0 || p.Volume > 0.05 {
		return map[string]any{"success": false, "error": "volume must be in 0.01–0.05 lots"}, nil
	}
	if p.Account == "" {
		p.Account = "primary"
	}

	// Derive SL/TP from the live tick when the human didn't specify.
	if p.SL == 0 || p.TP == 0 {
		tick, terr := engineGet("/mt5/tick/" + p.Symbol)
		if p.Account == "exness" {
			tick, terr = engineGet("/mt5/exness/tick/" + p.Symbol)
		}
		if terr != nil || tick == nil {
			return map[string]any{
				"success": false,
				"error":   "could not read live tick to derive SL/TP — engine/MT5 terminal not connected",
				"hint":    "start the engine (port 8001) and the MT5 terminal, or pass explicit sl/tp prices",
			}, nil
		}
		price := float64(0)
		if p.Type == "buy" {
			price = floatOf(tick, "ask")
		} else {
			price = floatOf(tick, "bid")
		}
		digits := int(floatOf(tick, "digits"))
		if digits == 0 {
			digits = 5
		}
		point := 0.0001
		if digits >= 3 {
			point = 0.01
		}
		if p.SL == 0 {
			if p.Type == "buy" {
				p.SL = math.Round((price-50*point)*math.Pow(10, float64(digits))) / math.Pow(10, float64(digits))
			} else {
				p.SL = math.Round((price+50*point)*math.Pow(10, float64(digits))) / math.Pow(10, float64(digits))
			}
		}
		if p.TP == 0 {
			rr := (price - p.SL) * 2 // 1:2 R:R
			if p.Type == "sell" {
				rr = (p.SL - price) * 2
			}
			tp := price - rr
			if p.Type == "buy" {
				tp = price + rr
			}
			p.TP = math.Round(tp*math.Pow(10, float64(digits))) / math.Pow(10, float64(digits))
		}
	}

	endpoint := "/mt5/order"
	if p.Account == "exness" {
		endpoint = "/mt5/exness/order"
	}
	result, err := enginePost(endpoint, map[string]any{
		"symbol": p.Symbol, "type": p.Type, "volume": p.Volume, "sl": p.SL, "tp": p.TP,
	})
	if err != nil {
		return map[string]any{
			"success": false, "account": p.Account, "error": err.Error(),
			"hint": "Is the engine on port 8001 and the MT5 terminal running?",
		}, nil
	}

	// The prop-firm engine returns 403 with a "daily profit cap" message when
	// blocked — surface that so the human knows it was refused, not filled.
	capBlocked := false
	if e, ok := result["error"]; ok {
		if s, ok := e.(string); ok && containsFold(s, "cap") {
			capBlocked = true
		}
	}

	return map[string]any{
		"success":      !capBlocked,
		"account":      p.Account,
		"symbol":       p.Symbol,
		"type":         p.Type,
		"volume":       p.Volume,
		"sl":           p.SL,
		"tp":           p.TP,
		"cap_blocked":  capBlocked,
		"result":       result,
		"note":         "Manual order placed via chat/voice — human chose lots/SL/TP; prop-firm daily cap + TP clamp applied by the gateway.",
	}, nil
}

func errInvalidArgs(msg string) error { return &invalidArgsError{msg: msg} }

type invalidArgsError struct{ msg string }

func (e *invalidArgsError) Error() string { return e.msg }

func containsFold(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if eqFold(s[i:i+len(sub)], sub) {
			return true
		}
	}
	return false
}

func eqFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca == cb {
			continue
		}
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

func init() {
	GlobalRegistry.Register(&ManualTradeTool{})
}
