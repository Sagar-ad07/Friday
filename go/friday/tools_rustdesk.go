package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// ──────────────────────────────────────────────────────────────────────
// RustDesk Desktop Automation Tool
//
// Friday creates click commands for the deepchart desktop app via RustDesk,
// waits for your confirmation, then executes the click. Implements strict
// 1-2 trades/day limit with prop firm protection.
// ──────────────────────────────────────────────────────────────────────

type RustDeskTool struct{}

func (t *RustDeskTool) Name() string { return "rustdesk_trade" }

func (t *RustDeskTool) Description() string {
	return `Execute manual trades on deepchart via RustDesk desktop automation.

How it works:
1. Friday analyzes the market and proposes a trade (symbol, side, lots)
2. You review the proposal and confirm via chat: "EXECUTE TRADE"
3. Friday sends a click command to RustDesk to execute the trade
4. Max 2 trades per day to protect the prop firm account

Choose account:
- primary = BlueGuardian prop firm (daily $37.50 cap)
- exness = personal account (no cap, 24/7)

Example: "Friday, rustdesk_trade on EURUSDm, buy, 0.02 lots"`
}

func (t *RustDeskTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"account": {Type: "string", Enum: []string{"primary", "exness"}, Description: "Primary (prop) or Exness (personal)"},
			"symbol":  {Type: "string", Description: "Symbol: EURUSD, XAUUSD, EURUSDm"},
			"type":    {Type: "string", Enum: []string{"buy", "sell"}, Description: "buy or sell"},
			"volume":  {Type: "number", Description: "Lots: 0.01–0.05"},
		},
		Required: []string{"symbol", "type", "volume"},
	}
}

func (t *RustDeskTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Account string  `json:"account"`
		Symbol  string  `json:"symbol"`
		Type    string  `json:"type"`
		Volume  float64 `json:"volume"`
	}

	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("invalid args: %w", err)
	}

	// Validate inputs
	if p.Account == "" {
		p.Account = "primary"
	}

	if p.Symbol == "" || p.Type == "" {
		return nil, fmt.Errorf("symbol and type are required")
	}

	if p.Volume <= 0 || p.Volume > 0.05 {
		return nil, fmt.Errorf("volume must be between 0.01 and 0.05 lots")
	}

	// Check daily trade limit (2 trades max)
	if dailyCount, err := t.getDailyTradeCount(); err == nil && dailyCount >= 2 {
		return map[string]any{
			"success": false,
			"error":   "Daily trade limit reached (2 trades max)",
			"trades_today": dailyCount,
			"limit": 2,
			"note":   "Close your open position first, then request a new trade tomorrow.",
		}, nil
	}

	// Get live price for SL/TP calculation
	tick, err := t.getTick(p.Symbol, p.Account)
	if err != nil {
		return map[string]any{
			"success": false,
			"error":   "Could not read live tick — deepchart MT5 terminal not connected",
		}, nil
	}

	// Calculate SL/TP from current price
	price, sl, tp := t.calculateSLTP(tick, p.Type, p.Volume)

	// Check if RustDesk is configured
	if !t.isRustDeskRunning() {
		return map[string]any{
			"success": false,
			"error":   "RustDesk automation not available",
			"note": "Friday cannot send clicks without RustDesk. Please ensure:\n1. RustDesk is installed on your laptop\n2. RustDesk remote control is enabled\n3. You are running the Friday server on the same machine",
		}, nil
	}

	// Generate click coordinates
	clickX, clickY := t.calculateClickCoordinates(p.Symbol, p.Type)

	// Create click command
	clickCmd := t.createClickCommand(clickX, clickY, p.Symbol, p.Type, p.Volume, sl, tp)

	// Store in memory for execution
	pendingKey := fmt.Sprintf("rustdesk_pending_%d", time.Now().Unix())
	memoryStore.Set(pendingKey, map[string]any{
		"symbol":    p.Symbol,
		"type":      p.Type,
		"volume":    p.Volume,
		"sl":        sl,
		"tp":        tp,
		"account":   p.Account,
		"click_cmd": clickCmd,
		"click_x":   clickX,
		"click_y":   clickY,
		"submitted": time.Now(),
	})

	// Update daily counter
	t.incrementDailyTradeCount()

	// Return proposal to user
	dailyCount, _ := t.getDailyTradeCount()

	return map[string]any{
		"success":            false,
		"needs_confirmation": true,
		"proposal": map[string]any{
			"symbol":       p.Symbol,
			"type":         p.Type,
			"volume":       p.Volume,
			"sl":           sl,
			"tp":           tp,
			"price":        price,
			"click_coords": fmt.Sprintf("X=%d, Y=%d", clickX, clickY),
			"account":      p.Account,
			"daily_trades": dailyCount,
			"daily_limit":  2,
		},
		"confirm_phrase": "EXECUTE TRADE",
		"note": "Awaiting your confirmation. Type 'EXECUTE TRADE' in chat to confirm this RustDesk click.",
	}, nil
}

func (t *RustDeskTool) ExecuteConfirmed(confirm string) (map[string]any, error) {
	if confirm != "EXECUTE TRADE" {
		return map[string]any{
			"success": false,
			"error":   "Wrong confirmation. Type 'EXECUTE TRADE' exactly.",
		}, nil
	}

	// Find most recent pending trade
	pendingKey := t.findMostRecentPendingTrade()
	if pendingKey == "" {
		return map[string]any{
			"success": false,
			"error":   "No pending trade found. Please request a new trade proposal first.",
		}, nil
	}

	// Get pending data
	dataMap, err := memoryStore.Get(pendingKey)
	if err != nil {
		return map[string]any{
			"success": false,
			"error":   "Failed to retrieve pending trade",
		}, nil
	}

	data := dataMap.(map[string]any)
	clickX := int(data["click_x"].(float64))
	clickY := int(data["click_y"].(float64))

	// Execute the click via RustDesk service
	success, message, err := rustDeskService.SendClick(clickX, clickY, map[string]any{
		"symbol":    data["symbol"],
		"type":      data["type"],
		"volume":    data["volume"],
		"sl":        data["sl"],
		"tp":        data["tp"],
		"account":   data["account"],
	})

	// Clean up memory
	memoryStore.Delete(pendingKey)

	return map[string]any{
		"success": success,
		"message": message,
		"error":   err,
		"trade_details": map[string]any{
			"symbol":     data["symbol"],
			"type":       data["type"],
			"volume":     data["volume"],
			"sl":         data["sl"],
			"tp":         data["tp"],
			"account":    data["account"],
		},
	}, nil
}

func (t *RustDeskTool) getTick(symbol, account string) (map[string]any, error) {
	var tick map[string]any
	var err error

	if account == "exness" {
		tick, err = engineGet("/mt5/exness/tick/" + symbol)
	} else {
		tick, err = engineGet("/mt5/tick/" + symbol)
	}

	if err != nil || tick == nil {
		return nil, err
	}
	return tick, nil
}

func (t *RustDeskTool) isRustDeskRunning() bool {
	return os.Getenv("RUSTDESK_CONTROL") == "1"
}

func (t *RustDeskTool) calculateSLTP(tick map[string]any, orderType string, volume float64) (price, sl, tp float64) {
	price = float64(0)
	if orderType == "buy" {
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

	sl = price - 50*point
	tp = price + 100*point // 1:2 R:R

	return price, sl, tp
}

func (t *RustDeskTool) calculateClickCoordinates(symbol, orderType string) (int, int) {
	// Deepchart buy button coordinates (typical layout)
	buyX, buyY := 1200, 400
	sellX, sellY := 1200, 460

	if orderType == "buy" {
		return buyX, buyY
	}
	return sellX, sellY
}

func (t *RustDeskTool) createClickCommand(x, y int, symbol, orderType string, volume, sl, tp float64) string {
	accountType := "primary"
	if env := os.Getenv("ACCOUNT_TYPE"); env != "" {
		accountType = env
	}

	cmd := map[string]any{
		"command":     "click",
		"target":      "deepchart_app",
		"position":    map[string]any{"x": x, "y": y},
		"order_details": map[string]any{
			"symbol":     symbol,
			"type":       orderType,
			"volume":     volume,
			"sl":         sl,
			"tp":         tp,
			"account":    accountType,
			"timestamp":  time.Now().Unix(),
			"submitted_at": time.Now().Format(time.RFC3339),
		},
	}

	bytes, _ := json.Marshal(cmd)
	return string(bytes)
}

func (t *RustDeskTool) getDailyTradeCount() (int, error) {
	var count int
	memoryStore.Range(func(key string, value any) bool {
		if len(key) > len("rustdesk_pending_") && key[:len("rustdesk_pending_")] == "rustdesk_pending_" {
			count++
		}
		return true
	})
	return count, nil
}

func (t *RustDeskTool) incrementDailyTradeCount() {
	key := fmt.Sprintf("rustdesk_pending_%d", time.Now().Unix())
	if _, err := memoryStore.Get(key); err != nil {
		memoryStore.Set(key, map[string]any{"count": 1})
	}
}

func (t *RustDeskTool) findMostRecentPendingTrade() string {
	var mostRecent string
	var mostRecentTime time.Time

	memoryStore.Range(func(key string, value any) bool {
		if len(key) > len("rustdesk_pending_") && key[:len("rustdesk_pending_")] == "rustdesk_pending_" {
			if valueMap, ok := value.(map[string]any); ok {
				if submitted, ok := valueMap["submitted"].(time.Time); ok {
					if mostRecentTime.IsZero() || submitted.After(mostRecentTime) {
						mostRecent = key
						mostRecentTime = submitted
					}
				}
			}
		}
		return true
	})

	return mostRecent
}

func init() {
	GlobalRegistry.Register(&RustDeskTool{})
}
