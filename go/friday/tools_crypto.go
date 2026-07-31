package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────
// Crypto Trading Tools (24/7) — real Binance data, paper trading
// ──────────────────────────────────────────────────────────────────────

type CryptoPriceTool struct{}

func (t *CryptoPriceTool) Name() string        { return "crypto_price" }
func (t *CryptoPriceTool) Description() string { return "Get current price of any crypto symbol from Binance (24/7 markets)" }
func (t *CryptoPriceTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"symbol": {Type: "string", Description: "Crypto symbol (e.g., BTCUSDT, ETHUSDT, SOLUSDT)", Default: "BTCUSDT"},
		},
		Required: []string{},
	}
}
func (t *CryptoPriceTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Symbol string `json:"symbol"`
	}
	if err := json.Unmarshal(args, &params); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }
	if params.Symbol == "" {
		params.Symbol = "BTCUSDT"
	}

	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("https://api.binance.com/api/v3/ticker/24hr?symbol=%s", strings.ToUpper(params.Symbol))
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("binance price: %w", err)
	}
	defer resp.Body.Close()

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("binance decode: %w", err)
	}

	price := toFloat(raw["lastPrice"])
	change := toFloat(raw["priceChangePercent"])
	high := toFloat(raw["highPrice"])
	low := toFloat(raw["lowPrice"])
	vol := toFloat(raw["volume"])

	return map[string]any{
		"symbol":          params.Symbol,
		"price":           price,
		"change_24h_pct":  math.Round(change*100) / 100,
		"high_24h":        high,
		"low_24h":         low,
		"volume_24h":      vol,
		"source":          "Binance (free, 24/7)",
		"timestamp":       time.Now().Unix(),
	}, nil
}

type CryptoPortfolioTool struct{}

func (t *CryptoPortfolioTool) Name() string        { return "crypto_portfolio" }
func (t *CryptoPortfolioTool) Description() string { return "Check crypto paper trading portfolio status, equity, and positions" }
func (t *CryptoPortfolioTool) Schema() ToolSchema {
	return ToolSchema{
		Type:       "object",
		Properties: map[string]PropertyDef{},
		Required:   []string{},
	}
}
func (t *CryptoPortfolioTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	result, err := engineGet("/crypto/portfolio")
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return result, nil
}

type CryptoGridTool struct{}

func (t *CryptoGridTool) Name() string        { return "crypto_grid" }
func (t *CryptoGridTool) Description() string { return "Start or check status of the automated grid trading strategy (24/7 profit capture)" }
func (t *CryptoGridTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"action":     {Type: "string", Enum: []string{"start", "stop", "status"}, Description: "Grid action"},
			"symbol":     {Type: "string", Description: "Trading pair (default: BTCUSDT)"},
			"grids":      {Type: "number", Description: "Number of grid levels (default: 10)"},
			"investment": {Type: "number", Description: "Total USDT to deploy (default: 500)"},
		},
		Required: []string{"action"},
	}
}
func (t *CryptoGridTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Action     string  `json:"action"`
		Symbol     string  `json:"symbol"`
		Grids      int     `json:"grids"`
		Investment float64 `json:"investment"`
	}
	if err := json.Unmarshal(args, &params); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }
	if params.Symbol == "" { params.Symbol = "BTCUSDT" }

	switch params.Action {
	case "start":
		return enginePost("/grid/start", params)
	case "stop":
		return enginePost("/grid/stop", params)
	case "status":
		return engineGet("/grid/status")
	default:
		return map[string]any{"error": "unknown action: " + params.Action}, nil
	}
}

type MarketRegimeTool struct{}

func (t *MarketRegimeTool) Name() string        { return "market_regime" }
func (t *MarketRegimeTool) Description() string { return "Analyze current market regime (trending/ranging/volatile) with indicators (RSI, ADX, ATR) and get trade recommendations" }
func (t *MarketRegimeTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"symbol":   {Type: "string", Description: "Crypto symbol (default: BTCUSDT)"},
			"interval": {Type: "string", Description: "Timeframe (1m, 5m, 15m, 1h, 4h, 1d)", Default: "1h"},
		},
		Required: []string{},
	}
}
func (t *MarketRegimeTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Symbol   string `json:"symbol"`
		Interval string `json:"interval"`
	}
	if err := json.Unmarshal(args, &params); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }
	if params.Symbol == "" {
		params.Symbol = "BTCUSDT"
	}
	if params.Interval == "" {
		params.Interval = "1h"
	}

	// Fetch klines from Binance
	client := &http.Client{Timeout: 15 * time.Second}
	url := fmt.Sprintf("https://api.binance.com/api/v3/klines?symbol=%s&interval=%s&limit=100",
		strings.ToUpper(params.Symbol), params.Interval)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("binance klines: %w", err)
	}
	defer resp.Body.Close()

	// Handle both array and error response from Binance
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if strings.Contains(string(bodyBytes), "code") {
		var errResp struct { Code int `json:"code"`; Msg string `json:"msg"` }
		if json.Unmarshal(bodyBytes, &errResp) == nil {
			return map[string]any{
				"symbol":   params.Symbol,
				"interval": params.Interval,
				"error":    fmt.Sprintf("Binance: %s", errResp.Msg),
				"note":     "EURUSD not on Binance. Try crypto pairs like BTCUSDT.",
			}, nil
		}
	}
	var raw [][]interface{}
	if err := json.Unmarshal(bodyBytes, &raw); err != nil {
		return map[string]any{
			"symbol":   params.Symbol,
			"interval": params.Interval,
			"error":    fmt.Sprintf("Data unavailable: %s. Try BTCUSDT or ETHUSDT.", err.Error()),
		}, nil
	}

	klines := make([]KlineData, 0, len(raw))
	for _, r := range raw {
		if len(r) < 5 {
			continue
		}
		klines = append(klines, KlineData{
			Open:  toFloat(r[1]),
			High:  toFloat(r[2]),
			Low:   toFloat(r[3]),
			Close: toFloat(r[4]),
		})
	}

	if len(klines) < 20 {
		return map[string]any{
			"symbol":   params.Symbol,
			"interval": params.Interval,
			"warning":  "Not enough data for regime analysis",
		}, nil
	}

	// Calculate indicators
	closes := make([]float64, len(klines))
	for i, k := range klines {
		closes[i] = k.Close
	}

	currentPrice := closes[len(closes)-1]
	rsi := calcRSI(closes, 14)
	sma20 := avg(closes[len(closes)-20:])
	sma50 := avg(closes)

	// Determine regime
	var regime string
	var rec string
	var confidence float64

	switch {
	case rsi > 70:
		regime = "possible_overbought"
		rec = "Caution — price may be overextended. Consider taking profit or waiting for pullback."
		confidence = 0.7
	case rsi < 30:
		regime = "possible_oversold"
		rec = "Potential buying opportunity — price may be undervalued. Wait for confirmation."
		confidence = 0.7
	case currentPrice > sma20 && sma20 > sma50:
		regime = "bullish_trend"
		rec = "Price above both SMAs — trend is up. Consider BUY on dips."
		confidence = 0.8
	case currentPrice < sma20 && sma20 < sma50:
		regime = "bearish_trend"
		rec = "Price below both SMAs — trend is down. Consider SELL on bounces."
		confidence = 0.8
	default:
		regime = "ranging"
		rec = "Price between SMAs — ranging market. Grid strategy recommended for consistent profit."
		confidence = 0.6
	}

	return map[string]any{
		"symbol":           params.Symbol,
		"interval":         params.Interval,
		"current_price":    currentPrice,
		"regime":           regime,
		"confidence":       confidence,
		"rsi_14":           math.Round(rsi*100) / 100,
		"sma_20":           math.Round(sma20*10000) / 10000,
		"sma_50":           math.Round(sma50*10000) / 10000,
		"recommendation":   rec,
		"source":           "Binance (free, 24/7)",
		"quote":            `"The best time to plant a tree was 20 years ago. The second best time is now."`,
	}, nil
}

type CryptoBacktestTool struct{}

func (t *CryptoBacktestTool) Name() string        { return "crypto_backtest" }
func (t *CryptoBacktestTool) Description() string { return "Run a quick backtest strategy comparison on crypto historical data" }
func (t *CryptoBacktestTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"symbol":   {Type: "string", Description: "Crypto symbol (default: BTCUSDT)"},
			"interval": {Type: "string", Description: "Timeframe (1m, 5m, 15m, 1h, 4h, 1d)", Default: "1h"},
		},
		Required: []string{},
	}
}
func (t *CryptoBacktestTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Symbol   string `json:"symbol"`
		Interval string `json:"interval"`
	}
	if err := json.Unmarshal(args, &params); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }
	if params.Symbol == "" { params.Symbol = "BTCUSDT" }
	if params.Interval == "" { params.Interval = "1h" }
	result, err := engineGet("/backtest/quick/" + url.PathEscape(params.Symbol) + "/" + url.PathEscape(params.Interval))
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return result, nil
}

type CryptoTradeTool struct{}

func (t *CryptoTradeTool) Name() string        { return "crypto_trade" }
func (t *CryptoTradeTool) Description() string { return "Execute a paper trade on crypto market (simulated, no real money)" }
func (t *CryptoTradeTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"symbol":   {Type: "string", Description: "Crypto symbol (e.g., BTCUSDT)"},
			"side":     {Type: "string", Enum: []string{"BUY", "SELL"}, Description: "Trade direction"},
			"quantity": {Type: "number", Description: "Amount to trade (e.g., 0.01 BTC)"},
		},
		Required: []string{"symbol", "side"},
	}
}
func (t *CryptoTradeTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Symbol   string  `json:"symbol"`
		Side     string  `json:"side"`
		Quantity float64 `json:"quantity"`
	}
	if err := json.Unmarshal(args, &params); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }
	result, err := enginePost("/crypto/trade", params)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	return result, nil
}
