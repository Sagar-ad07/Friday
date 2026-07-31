package trading

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ──────────────────────────────────────────────────────────────────────
// Binance Client — free 24/7 market data (no API key needed for reads)
// ──────────────────────────────────────────────────────────────────────

type Kline struct {
	OpenTime  int
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	CloseTime int
}

type Ticker struct {
	Symbol      string  `json:"symbol"`
	Price       float64 `json:"-"`
	PriceChange float64 `json:"priceChange"`
	High24      float64 `json:"-"`
	Low24       float64 `json:"-"`
	Volume24    float64 `json:"volume"`
}

type BinanceClient struct {
	baseURL string
	client  *http.Client
}

func NewBinanceClient() *BinanceClient {
	return &BinanceClient{
		baseURL: "https://api.binance.com",
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// GetPrice returns the current price of a symbol (e.g., BTCUSDT)
func (b *BinanceClient) GetPrice(ctx context.Context, symbol string) (float64, error) {
	url := fmt.Sprintf("%s/api/v3/ticker/price?symbol=%s", b.baseURL, strings.ToUpper(symbol))
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := b.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("binance price: %w", err)
	}
	defer resp.Body.Close()

	var data struct {
		Symbol string `json:"symbol"`
		Price  string `json:"price"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, fmt.Errorf("binance price decode: %w", err)
	}
	return strconv.ParseFloat(data.Price, 64)
}

// GetKlines returns candlestick data for backtesting and analysis
func (b *BinanceClient) GetKlines(ctx context.Context, symbol, interval string, limit int) ([]Kline, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	url := fmt.Sprintf("%s/api/v3/klines?symbol=%s&interval=%s&limit=%d",
		b.baseURL, strings.ToUpper(symbol), interval, limit)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("binance klines: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var raw [][]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("binance klines decode: %w", err)
	}

	klines := make([]Kline, 0, len(raw))
	for _, r := range raw {
		if len(r) < 11 {
			continue
		}
		k := Kline{
			OpenTime:  toInt(r[0]),
			Open:      toFloat(r[1]),
			High:      toFloat(r[2]),
			Low:       toFloat(r[3]),
			Close:     toFloat(r[4]),
			Volume:    toFloat(r[5]),
			CloseTime: toInt(r[6]),
		}
		klines = append(klines, k)
	}
	return klines, nil
}

// Get24hrTicker returns 24hr rolling ticker data
func (b *BinanceClient) Get24hrTicker(ctx context.Context, symbol string) (*Ticker, error) {
	url := fmt.Sprintf("%s/api/v3/ticker/24hr?symbol=%s", b.baseURL, strings.ToUpper(symbol))
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("binance 24hr: %w", err)
	}
	defer resp.Body.Close()

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("binance 24hr decode: %w", err)
	}

	t := &Ticker{
		Symbol:      getStr(raw, "symbol"),
		PriceChange: toFloat(raw["priceChange"]),
		Volume24:    toFloat(raw["volume"]),
	}
	t.Price = toFloat(raw["lastPrice"])
	t.High24 = toFloat(raw["highPrice"])
	t.Low24 = toFloat(raw["lowPrice"])
	return t, nil
}

// ──────────────────────────────────────────────────────────────────────
// Paper Crypto Portfolio — tracks virtual positions
// ──────────────────────────────────────────────────────────────────────

type CryptoOrder struct {
	ID        string    `json:"id"`
	Symbol    string    `json:"symbol"`
	Side      string    `json:"side"` // BUY or SELL
	Type      string    `json:"type"` // MARKET or LIMIT or GRID
	Price     float64   `json:"price"`
	Quantity  float64   `json:"quantity"`
	Filled    float64   `json:"filled"`
	Status    string    `json:"status"` // OPEN, FILLED, CANCELLED
	PnL       float64   `json:"pnl"`
	CreatedAt time.Time `json:"created_at"`
	FilledAt  time.Time `json:"filled_at,omitempty"`
}

type CryptoPortfolio struct {
	mu      sync.RWMutex
	Balance float64       `json:"balance"`   // USDT balance
	Holdings map[string]float64 `json:"holdings"` // symbol -> quantity
	Orders  []CryptoOrder `json:"orders"`
	Trades  int           `json:"trades"`
	WinRate float64       `json:"win_rate"`
	TotalPnL float64      `json:"total_pnl"`
}

func NewCryptoPortfolio(balance float64) *CryptoPortfolio {
	return &CryptoPortfolio{
		Balance:  balance,
		Holdings: make(map[string]float64),
		Orders:   make([]CryptoOrder, 0),
	}
}

func (p *CryptoPortfolio) PlaceOrder(symbol, side string, price, quantity float64) *CryptoOrder {
	p.mu.Lock()
	defer p.mu.Unlock()

	cost := price * quantity
	order := CryptoOrder{
		ID:        fmt.Sprintf("crypto_%d", time.Now().UnixNano()),
		Symbol:    symbol,
		Side:      side,
		Type:      "MARKET",
		Price:     price,
		Quantity:  quantity,
		Status:    "FILLED",
		CreatedAt: time.Now(),
		FilledAt:  time.Now(),
	}

	if side == "BUY" {
		if p.Balance < cost {
			order.Status = "CANCELLED"
			p.Orders = append(p.Orders, order)
			return &order
		}
		p.Balance -= cost
		p.Holdings[symbol] += quantity
	} else { // SELL
		if p.Holdings[symbol] < quantity {
			order.Status = "CANCELLED"
			p.Orders = append(p.Orders, order)
			return &order
		}
		p.Holdings[symbol] -= quantity
		p.Balance += cost
		// Track PnL for sell orders
		order.PnL = cost // simplified
	}

	p.Trades++
	p.Orders = append(p.Orders, order)
	return &order
}

func (p *CryptoPortfolio) GetEquity(currentPrices map[string]float64) float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()

	equity := p.Balance
	for sym, qty := range p.Holdings {
		if price, ok := currentPrices[sym]; ok {
			equity += price * qty
		}
	}
	return equity
}

func (p *CryptoPortfolio) GetSummary() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return map[string]interface{}{
		"balance":   p.Balance,
		"holdings":  p.Holdings,
		"trades":    p.Trades,
		"total_pnl": p.TotalPnL,
		"orders":    len(p.Orders),
	}
}

// ──────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────

func toFloat(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	case int:
		return float64(val)
	}
	return 0
}

func toInt(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	case string:
		i, _ := strconv.Atoi(val)
		return i
	}
	return 0
}

func getStr(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// ──────────────────────────────────────────────────────────────────────
// Market Regime Detection — tells us WHEN to trade
// ──────────────────────────────────────────────────────────────────────

type MarketRegime string

const (
	RegimeTrending   MarketRegime = "trending"
	RegimeRanging    MarketRegime = "ranging"
	RegimeVolatile   MarketRegime = "volatile"
	RegimeQuiet      MarketRegime = "quiet"
)

type RegimeResult struct {
	Regime      MarketRegime `json:"regime"`
	Confidence  float64      `json:"confidence"`  // 0-1
	ADX         float64      `json:"adx"`
	ATR         float64      `json:"atr"`
	ATRPercent  float64      `json:"atr_percent"`
	RSI         float64      `json:"rsi"`
	Recommendation string   `json:"recommendation"`
}

// DetectRegime analyzes klines and returns the current market regime
func DetectRegime(klines []Kline) *RegimeResult {
	if len(klines) < 20 {
		return &RegimeResult{Regime: RegimeQuiet, Confidence: 0, Recommendation: "Not enough data"}
	}

	closes := make([]float64, len(klines))
	highs := make([]float64, len(klines))
	lows := make([]float64, len(klines))
	for i, k := range klines {
		closes[i] = k.Close
		highs[i] = k.High
		lows[i] = k.Low
	}

	// ATR (Average True Range) — volatility
	atr := calcATR(highs, lows, closes, 14)
	avgPrice := avg(closes)
	atrPercent := (atr / avgPrice) * 100

	// ADX — trend strength
	adx := calcADX(highs, lows, closes, 14)

	// RSI — overbought/oversold
	rsi := calcRSI(closes, 14)

	// Determine regime
	var regime MarketRegime
	confidence := 0.5

	switch {
	case adx > 25 && atrPercent > 2:
		regime = RegimeTrending
		confidence = math.Min(0.9, adx/50)
	case adx < 20 && atrPercent < 1.5:
		regime = RegimeRanging
		confidence = 0.7
	case atrPercent > 3:
		regime = RegimeVolatile
		confidence = 0.8
	default:
		regime = RegimeQuiet
		confidence = 0.5
	}

	// Generate recommendation
	rec := "Neutral — observe"
	switch regime {
	case RegimeTrending:
		if rsi < 40 {
			rec = "Bullish trend detected — consider BUY on pullback"
		} else if rsi > 60 {
			rec = "Strong uptrend — hold or take partial profit"
		} else {
			rec = "Trend developing — wait for confirmation"
		}
	case RegimeRanging:
		if rsi < 30 {
			rec = "Oversold in range — consider BUY near support"
		} else if rsi > 70 {
			rec = "Overbought in range — consider SELL near resistance"
		} else {
			rec = "Ranging market — grid strategy recommended"
		}
	case RegimeVolatile:
		rec = "High volatility — reduce position size, widen stops"
	case RegimeQuiet:
		rec = "Low activity — wait for setup"
	}

	return &RegimeResult{
		Regime:      regime,
		Confidence:  math.Round(confidence*100) / 100,
		ADX:         math.Round(adx*100) / 100,
		ATR:         math.Round(atr*10000) / 10000,
		ATRPercent:  math.Round(atrPercent*100) / 100,
		RSI:         math.Round(rsi*100) / 100,
		Recommendation: rec,
	}
}

// ── Technical indicators ──

func calcATR(high, low, close []float64, period int) float64 {
	if len(high) < period+1 {
		return 0
	}
	tr := make([]float64, len(high)-1)
	for i := 1; i < len(high); i++ {
		hilo := high[i] - low[i]
		hc := math.Abs(high[i] - close[i-1])
		lc := math.Abs(low[i] - close[i-1])
		tr[i-1] = math.Max(hilo, math.Max(hc, lc))
	}
	return ema(tr, period)[len(tr)-1]
}

func calcADX(high, low, close []float64, period int) float64 {
	if len(high) < period*2 {
		return 0
	}
	// +DI and -DI
	plusDM := make([]float64, len(high)-1)
	minusDM := make([]float64, len(high)-1)
	tr := make([]float64, len(high)-1)

	for i := 1; i < len(high); i++ {
		upMove := high[i] - high[i-1]
		downMove := low[i-1] - low[i]
		if upMove > downMove && upMove > 0 {
			plusDM[i-1] = upMove
		}
		if downMove > upMove && downMove > 0 {
			minusDM[i-1] = downMove
		}
		hilo := high[i] - low[i]
		hc := math.Abs(high[i] - close[i-1])
		lc := math.Abs(low[i] - close[i-1])
		tr[i-1] = math.Max(hilo, math.Max(hc, lc))
	}

	emaTR := ema(tr, period)
	emaPlus := ema(plusDM, period)
	emaMinus := ema(minusDM, period)

	lastTR := emaTR[len(emaTR)-1]
	if lastTR == 0 {
		return 0
	}
	plusDI := (emaPlus[len(emaPlus)-1] / lastTR) * 100
	minusDI := (emaMinus[len(emaMinus)-1] / lastTR) * 100

	dx := math.Abs(plusDI-minusDI) / (plusDI + minusDI) * 100
	return dx
}

func calcRSI(prices []float64, period int) float64 {
	if len(prices) < period+1 {
		return 50
	}
	gains, losses := 0.0, 0.0
	for i := 1; i <= period; i++ {
		diff := prices[i] - prices[i-1]
		if diff > 0 {
			gains += diff
		} else {
			losses -= diff
		}
	}
	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)
	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}

func ema(values []float64, period int) []float64 {
	if len(values) == 0 || period <= 0 {
		return nil
	}
	result := make([]float64, len(values))
	multiplier := 2.0 / float64(period+1)

	// SMA for first value
	sum := 0.0
	for i := 0; i < period && i < len(values); i++ {
		sum += values[i]
	}
	result[period-1] = sum / float64(period)

	for i := period; i < len(values); i++ {
		result[i] = (values[i]-result[i-1])*multiplier + result[i-1]
	}
	return result
}

func avg(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// ── Trade suggestion based on regime + indicators ──

type TradeSignal struct {
	Action      string  `json:"action"`       // BUY, SELL, HOLD
	Symbol      string  `json:"symbol"`
	Confidence  float64 `json:"confidence"`   // 0-1
	Reason      string  `json:"reason"`
	EntryPrice  float64 `json:"entry_price"`
	StopLoss    float64 `json:"stop_loss"`
	TakeProfit  float64 `json:"take_profit"`
	PositionPct float64 `json:"position_pct"` // % of portfolio to risk
}

func GenerateSignal(symbol string, klines []Kline, portfolio *CryptoPortfolio) *TradeSignal {
	regime := DetectRegime(klines)
	if len(klines) < 20 {
		return &TradeSignal{Action: "HOLD", Symbol: symbol, Reason: "Not enough data"}
	}

	closes := make([]float64, len(klines))
	for i, k := range klines {
		closes[i] = k.Close
	}

	currentPrice := closes[len(closes)-1]
	rsi := calcRSI(closes, 14)
	sma20 := avg(closes[len(closes)-20:])
	sma50 := avg(closes)

	// Generate signal based on regime + indicators
	signal := &TradeSignal{
		Symbol:     symbol,
		EntryPrice: currentPrice,
		Confidence: regime.Confidence,
	}

	switch regime.Regime {
	case RegimeTrending:
		if rsi < 35 && currentPrice < sma20 {
			signal.Action = "BUY"
			signal.Reason = fmt.Sprintf("Oversold in uptrend (RSI=%.0f)", rsi)
			signal.StopLoss = currentPrice * 0.97
			signal.TakeProfit = currentPrice * 1.05
			signal.PositionPct = 15
		} else if rsi > 65 && currentPrice > sma20*1.03 {
			signal.Action = "SELL"
			signal.Reason = fmt.Sprintf("Overbought in downtrend (RSI=%.0f)", rsi)
			signal.StopLoss = currentPrice * 1.03
			signal.TakeProfit = currentPrice * 0.95
			signal.PositionPct = 10
		} else {
			signal.Action = "HOLD"
			signal.Reason = "Trend intact, waiting for entry"
		}

	case RegimeRanging:
		if rsi < 30 {
			signal.Action = "BUY"
			signal.Reason = fmt.Sprintf("Oversold at range support (RSI=%.0f)", rsi)
			signal.StopLoss = currentPrice * 0.96
			signal.TakeProfit = currentPrice * 1.04
			signal.PositionPct = 20
		} else if rsi > 70 {
			signal.Action = "SELL"
			signal.Reason = fmt.Sprintf("Overbought at range resistance (RSI=%.0f)", rsi)
			signal.StopLoss = currentPrice * 1.04
			signal.TakeProfit = currentPrice * 0.96
			signal.PositionPct = 15
		} else {
			signal.Action = "HOLD"
			signal.Reason = "Range middle — grid strategy recommended"
			signal.PositionPct = 5
		}

	case RegimeVolatile:
		signal.Action = "HOLD"
		signal.Reason = fmt.Sprintf("High volatility (ATR=%.2f%%) — reducing risk", regime.ATRPercent)
		signal.PositionPct = 5

	default:
		signal.Action = "HOLD"
		signal.Reason = "Low activity — monitoring"
		signal.PositionPct = 0
	}

	// Reduce confidence if price is moving against
	if signal.Action == "BUY" && currentPrice < sma50 {
		signal.Confidence *= 0.7
		signal.Reason += " (below 50 SMA — caution)"
	}
	if signal.Action == "SELL" && currentPrice > sma50 {
		signal.Confidence *= 0.7
		signal.Reason += " (above 50 SMA — caution)"
	}

	signal.Confidence = math.Round(signal.Confidence*100) / 100
	signal.EntryPrice = math.Round(currentPrice*10000) / 10000
	signal.StopLoss = math.Round(signal.StopLoss*10000) / 10000
	signal.TakeProfit = math.Round(signal.TakeProfit*10000) / 10000

	return signal
}

// ── Sorted map keys helper ──

func sortedKeys(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}
