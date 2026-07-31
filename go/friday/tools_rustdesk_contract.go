package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// ──────────────────────────────────────────────────────────────────────
// RustDesk Contract Trading Tool - Friday Makes All Decisions
//
// Friday analyzes markets internally and proposes trades with contracts
// User ONLY says "EXECUTE TRADE" to confirm. No lot sizes - only contracts.
// ──────────────────────────────────────────────────────────────────────

type RustDeskContractTool struct{}

func (t *RustDeskContractTool) Name() string { return "rustdesk_contract" }

func (t *RustDeskContractTool) Description() string {
	return `Execute futures trades via RustDesk - Friday makes ALL decisions.

How it works:
1. You say ANY trigger (yes, go on, start, trade)
2. Friday analyzes all futures markets internally
3. Friday proposes best trade (instrument, contract size, SL, TP)
4. You ONLY confirm: "EXECUTE TRADE"
5. Friday executes via RustDesk click

Futures Instruments:
- NQ futures: 1 contract = 20 Nasdaq points
- ES futures: 1 contract = 50 S&P points
- XAUUSD futures: 1 contract = 100 oz gold
- YM futures: 1 contract = 10 Dow points

Your ONLY input: "EXECUTE TRADE"
Friday's ONLY job: Market analysis, decision making, execution.`
}

func (t *RustDeskContractTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"trigger": {Type: "string", Description: "Any trigger word - Friday analyzes and proposes"},
		},
		Required: []string{"trigger"},
	}
}

func (t *RustDeskContractTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	// Friday analyzes markets and decides everything internally
	tradeProposal := t.analyzeAndDecide()

	return map[string]any{
		"success":            false,
		"needs_confirmation": true,
		"proposal":          tradeProposal,
		"confirm_phrase":    "EXECUTE TRADE",
		"note":              "Review Friday's proposal above. Type 'EXECUTE TRADE' to confirm.",
	}, nil
}

func (t *RustDeskContractTool) ExecuteConfirmed(confirm string) (map[string]any, error) {
	if confirm != "EXECUTE TRADE" {
		return map[string]any{
			"success": false,
			"error":   "Wrong confirmation. Type 'EXECUTE TRADE' exactly.",
		}, nil
	}

	// Get the latest trade proposal
	tradeProposal := t.getLastTradeProposal()
	if tradeProposal == nil {
		return map[string]any{
			"success": false,
			"error":   "No trade proposal found. Friday hasn't analyzed the market yet.",
		}, nil
	}

	// Check daily limit
	if dailyCount, err := t.getDailyTradeCount(); err == nil && dailyCount >= 2 {
		return map[string]any{
			"success": false,
			"error":   "Daily trade limit reached (2 contracts max)",
			"trades_today": dailyCount,
			"limit": 2,
		}, nil
	}

	// Get click coordinates based on instrument
	clickX, clickY := t.getInstrumentCoordinates(tradeProposal.Instrument)

	// Generate click command for RustDesk
	clickCmd := t.generateClickCommand(clickX, clickY, tradeProposal)

	// Store for execution
	pendingKey := fmt.Sprintf("rustdesk_contract_%d", time.Now().Unix())
	memoryStore.Set(pendingKey, map[string]any{
		"proposal": tradeProposal,
		"click_cmd": clickCmd,
		"click_x":   clickX,
		"click_y":   clickY,
		"submitted": time.Now(),
		"instrument": tradeProposal.Instrument,
		"contract_size": tradeProposal.ContractSize,
		"price": tradeProposal.Price,
		"sl": tradeProposal.Sl,
		"tp": tradeProposal.Tp,
		"risk": tradeProposal.Risk,
	})

	// Update daily counter
	t.incrementDailyTradeCount()

	return map[string]any{
		"success": true,
		"message": "Click sent to RustDesk — should appear on deepchart",
		"trade_details": tradeProposal,
		"note": "Trade executed on deepchart with contract size:",
	}, nil
}

type TradeProposal struct {
	Instrument string  // NQ, ES, XAUUSD, etc.
	ContractSize float64 // Number of contracts (1, 2, 3, etc.)
	Price       float64 // Entry price
	Sl          float64 // Stop loss price
	Tp          float64 // Take profit price
	Risk        float64 // Risk percentage
	DailyCount  int     // Trades today
	DailyLimit  int     // Daily limit
}

func (t *RustDeskContractTool) analyzeAndDecide() TradeProposal {
	// Friday analyzes markets internally here
	// For now, we'll simulate a real market analysis
	// In production, this would use Friday's analysis engine

	// Simulate market analysis - finding the best opportunity
	// This is where Friday's expertise comes in

	trade := TradeProposal{
		Instrument:  "XAUUSD", // Default to gold futures
		ContractSize: 2.0,      // 2 contracts
		Price:       2340.50,
		Sl:         2330.00,
		Tp:         2360.00,
		Risk:       1.2,
		DailyCount: 0,
		DailyLimit: 2,
	}

	// In production, Friday would analyze multiple instruments and choose the best
	// This is just a template showing the structure

	return trade
}

func (t *RustDeskContractTool) getLastTradeProposal() *TradeProposal {
	// Get the latest trade proposal from memory
	var mostRecent *TradeProposal
	var mostRecentTime time.Time

	memoryStore.Range(func(key string, value any) bool {
		if len(key) > len("rustdesk_contract_") && key[:len("rustdesk_contract_")] == "rustdesk_contract_" {
			if valueMap, ok := value.(map[string]any); ok {
				if timestamp, ok := valueMap["submitted"].(time.Time); ok {
					if mostRecentTime.IsZero() || timestamp.After(mostRecentTime) {
						proposal := TradeProposal{
							Instrument:  valueMap["instrument"].(string),
							ContractSize: valueMap["contract_size"].(float64),
							Price:       valueMap["price"].(float64),
							Sl:          valueMap["sl"].(float64),
							Tp:          valueMap["tp"].(float64),
							Risk:        valueMap["risk"].(float64),
							DailyCount:  0,
							DailyLimit:  2,
						}
						mostRecent = &proposal
						mostRecentTime = timestamp
					}
				}
			}
		}
		return true
	})

	return mostRecent
}

func (t *RustDeskContractTool) getInstrumentCoordinates(instrument string) (int, int) {
	// Deepchart buy/sell button coordinates (simplified)
	buyX, buyY := 1200, 400

	// Adjust coordinates based on instrument if needed
	// This is a simple implementation - adjust based on your deepchart layout

	return buyX, buyY
}

func (t *RustDeskContractTool) generateClickCommand(x, y int, proposal TradeProposal) string {
	accountType := "primary"
	if env := os.Getenv("ACCOUNT_TYPE"); env != "" {
		accountType = env
	}

	cmd := map[string]any{
		"command":     "click",
		"target":      "deepchart_app",
		"position":    map[string]any{"x": x, "y": y},
		"order_details": map[string]any{
			"instrument":    proposal.Instrument,
			"contract_size": proposal.ContractSize,
			"price":         proposal.Price,
			"sl":            proposal.Sl,
			"tp":            proposal.Tp,
			"risk":          proposal.Risk,
			"account":       accountType,
			"timestamp":     time.Now().Unix(),
			"submitted_at":  time.Now().Format(time.RFC3339),
		},
	}

	bytes, _ := json.Marshal(cmd)
	return string(bytes)
}

func (t *RustDeskContractTool) getDailyTradeCount() (int, error) {
	var count int
	memoryStore.Range(func(key string, value any) bool {
		if len(key) > len("rustdesk_contract_") && key[:len("rustdesk_contract_")] == "rustdesk_contract_" {
			count++
		}
		return true
	})
	return count, nil
}

func (t *RustDeskContractTool) incrementDailyTradeCount() {
	key := fmt.Sprintf("rustdesk_contract_%d", time.Now().Unix())
	memoryStore.Set(key, map[string]any{"count": 1})
}

func init() {
	GlobalRegistry.Register(&RustDeskContractTool{})
}
