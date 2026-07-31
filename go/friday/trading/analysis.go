package trading

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// ──────────────────────────────────────────────────────────────────────
// Multi-Timeframe Consensus — only trade when ALL timeframes agree
// Higher probability = fewer but better trades
// ──────────────────────────────────────────────────────────────────────

type TFConsensus struct {
	Timeframes   []string `json:"timeframes"`
	Signals      []*TradeSignal `json:"signals"`
	Agreement    float64  `json:"agreement"`     // 0.0 - 1.0
	ConsensusAction string `json:"consensus_action"` // BUY, SELL, HOLD
	Confidence   float64  `json:"confidence"`    // 0-1
}

// AnalyzeMultiTF fetches klines for multiple timeframes and produces a consensus signal.
// Only returns BUY/SELL when agreement >= minAgreement (e.g., 0.66).
func AnalyzeMultiTF(symbol string, timeframes []string, portfolio *CryptoPortfolio, minAgreement float64) *TFConsensus {
	client := NewBinanceClient()
	result := &TFConsensus{
		Timeframes: timeframes,
		Signals:    make([]*TradeSignal, 0),
	}

	if len(timeframes) == 0 {
		timeframes = []string{"15m", "1h", "4h"}
	}
	if minAgreement <= 0 {
		minAgreement = 0.66
	}

	buyCount := 0
	sellCount := 0
	holdCount := 0
	totalConf := 0.0

	for _, tf := range timeframes {
		ctx, cancel := contextWithTimeout(10 * time.Second)
		klines, err := client.GetKlines(ctx, symbol, tf, 100)
		cancel()
		if err != nil {
			log.Printf("[MULTI-TF] %s %s fetch failed: %v", symbol, tf, err)
			continue
		}
		if len(klines) < 30 {
			continue
		}

		signal := GenerateSignal(symbol, klines, portfolio)
		if signal == nil {
			signal = &TradeSignal{Action: "HOLD", Symbol: symbol, Confidence: 0}
		}
		signal.Reason = fmt.Sprintf("[%s] %s", tf, signal.Reason)
		result.Signals = append(result.Signals, signal)
		totalConf += signal.Confidence

		switch signal.Action {
		case "BUY":
			buyCount++
		case "SELL":
			sellCount++
		default:
			holdCount++
		}
	}

	total := len(result.Signals)
	if total == 0 {
		result.ConsensusAction = "HOLD"
		result.Confidence = 0
		return result
	}

	buyPct := float64(buyCount) / float64(total)
	sellPct := float64(sellCount) / float64(total)

	switch {
	case buyPct >= minAgreement:
		result.ConsensusAction = "BUY"
		result.Agreement = buyPct
		result.Confidence = (buyPct + totalConf/float64(total)) / 2
	case sellPct >= minAgreement:
		result.ConsensusAction = "SELL"
		result.Agreement = sellPct
		result.Confidence = (sellPct + totalConf/float64(total)) / 2
	default:
		result.ConsensusAction = "HOLD"
		result.Agreement = math.Max(buyPct, sellPct)
		result.Confidence = result.Agreement * 0.5
	}

	result.Confidence = math.Min(math.Round(result.Confidence*100)/100, 0.99)
	result.Agreement = math.Round(result.Agreement*100) / 100
	return result
}

func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// ──────────────────────────────────────────────────────────────────────
// Kelly Criterion Position Sizing — maximizes long-term growth
// f* = (p * b - q) / b  where p=win prob, b=odds, q=lose prob
// ──────────────────────────────────────────────────────────────────────

type KellySizer struct {
	WinRate      float64 `json:"win_rate"`
	AvgWin       float64 `json:"avg_win"`       // as ratio (e.g., 2.0 = 2:1 reward)
	AvgLoss      float64 `json:"avg_loss"`       // as ratio (e.g., 1.0 = 1:1 risk)
	KellyFraction float64 `json:"kelly_fraction"` // full Kelly
	HalfKelly     float64 `json:"half_kelly"`     // conservative: half of Kelly
	QuarterKelly  float64 `json:"quarter_kelly"`  // very conservative
	RiskPerTradePct float64 `json:"risk_per_trade_pct"` // recommended % of portfolio
}

func CalculateKelly(winRate, avgWinRatio, avgLossRatio float64) *KellySizer {
	// f* = (p * b - q) / b
	// b = avgWin / avgLoss (odds ratio)
	b := avgWinRatio / avgLossRatio
	if b <= 0 || avgLossRatio <= 0 {
		return &KellySizer{
			WinRate: winRate, AvgWin: avgWinRatio, AvgLoss: avgLossRatio,
			KellyFraction: 0, HalfKelly: 0, QuarterKelly: 0,
			RiskPerTradePct: 1.0, // Default to 1% if no data
		}
	}
	q := 1 - winRate
	fStar := (winRate*b - q) / b
	if fStar < 0 {
		fStar = 0
	}
	if fStar > 0.5 {
		fStar = 0.5 // Cap at 50% for safety
	}

	return &KellySizer{
		WinRate:      math.Round(winRate*10000) / 100,
		AvgWin:       math.Round(avgWinRatio*100) / 100,
		AvgLoss:      math.Round(avgLossRatio*100) / 100,
		KellyFraction: math.Round(fStar*10000) / 100,
		HalfKelly:     math.Round(fStar/2*10000) / 100,
		QuarterKelly:  math.Round(fStar/4*10000) / 100,
		RiskPerTradePct: math.Round(fStar/2*10000) / 100, // Default to half-Kelly
	}
}

// ──────────────────────────────────────────────────────────────────────
// Trade Journal — persistent storage of every trade for learning
// ──────────────────────────────────────────────────────────────────────

type JournalEntry struct {
	ID         string    `json:"id"`
	Symbol     string    `json:"symbol"`
	Side       string    `json:"side"`
	EntryPrice float64   `json:"entry_price"`
	ExitPrice  float64   `json:"exit_price,omitempty"`
	Quantity   float64   `json:"quantity"`
	PnL        float64   `json:"pnl"`
	PnLPercent float64   `json:"pnl_percent"`
	EntryTime  time.Time `json:"entry_time"`
	ExitTime   time.Time `json:"exit_time,omitempty"`
	Regime     string    `json:"regime"`
	RSI        float64   `json:"rsi"`
	Confidence float64   `json:"confidence"`
	Strategy   string    `json:"strategy"`
	Win        bool      `json:"win"`
}

type TradeJournal struct {
	mu       sync.RWMutex
	Entries  []JournalEntry `json:"entries"`
	filePath string
}

func NewTradeJournal(projectRoot string) *TradeJournal {
	dir := filepath.Join(projectRoot, "data", "trades")
	os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, "journal.json")

	j := &TradeJournal{
		Entries:  make([]JournalEntry, 0),
		filePath: path,
	}
	j.load()
	return j
}

func (j *TradeJournal) Record(entry JournalEntry) {
	j.mu.Lock()
	defer j.mu.Unlock()
	entry.ID = fmt.Sprintf("trade_%d", time.Now().UnixNano())
	j.Entries = append(j.Entries, entry)
	j.save()
}

func (j *TradeJournal) Stats() map[string]interface{} {
	j.mu.RLock()
	defer j.mu.RUnlock()

	if len(j.Entries) == 0 {
		return map[string]interface{}{
			"total_trades": 0, "win_rate": 0, "avg_pnl": 0,
			"best_trade": 0, "worst_trade": 0, "total_pnl": 0,
		}
	}

	wins := 0
	totalPnL := 0.0
	best := -1e9
	worst := 1e9
	regimeWins := make(map[string]int)
	regimeTotal := make(map[string]int)

	for _, e := range j.Entries {
		totalPnL += e.PnL
		if e.PnL > best {
			best = e.PnL
		}
		if e.PnL < worst {
			worst = e.PnL
		}
		if e.Win {
			wins++
		}
		regimeTotal[e.Regime]++
		if e.Win {
			regimeWins[e.Regime]++
		}
	}

	winRate := float64(wins) / float64(len(j.Entries)) * 100

	// Regime-specific win rates
	regimeStats := make(map[string]float64)
	for r, total := range regimeTotal {
		if w, ok := regimeWins[r]; ok {
			regimeStats[r] = math.Round(float64(w)/float64(total)*10000) / 100
		}
	}

	return map[string]interface{}{
		"total_trades":  len(j.Entries),
		"win_rate":      math.Round(winRate*100) / 100,
		"avg_pnl":       math.Round(totalPnL/float64(len(j.Entries))*100) / 100,
		"best_trade":    math.Round(best*100) / 100,
		"worst_trade":   math.Round(worst*100) / 100,
		"total_pnl":     math.Round(totalPnL*100) / 100,
		"regime_win_rates": regimeStats,
	}
}

func (j *TradeJournal) load() {
	data, err := os.ReadFile(j.filePath)
	if err != nil {
		return
	}
	json.Unmarshal(data, &j.Entries)
}

func (j *TradeJournal) save() {
	data, _ := json.MarshalIndent(j.Entries, "", "  ")
	os.WriteFile(j.filePath, data, 0644)
}

// ──────────────────────────────────────────────────────────────────────
// Strategy Optimizer — brute-force parameter sweep
// Finds the best configuration for YOUR symbol
// ──────────────────────────────────────────────────────────────────────

type OptimizerConfig struct {
	Symbol      string   `json:"symbol"`
	Interval    string   `json:"interval"`
	Capital     float64  `json:"capital"`
	Commission  float64  `json:"commission"`
	Slippage    float64  `json:"slippage"`
	RSIPeriods  []int    `json:"rsi_periods"`
	SMAWindows  []int    `json:"sma_windows"`
	AtrPeriods  []int    `json:"atr_periods"`
}

type OptimizedConfig struct {
	RSIPeriod   int     `json:"rsi_period"`
	SMAWindow   int     `json:"sma_window"`
	ATRPeriod   int     `json:"atr_period"`
	TotalPnL    float64 `json:"total_pnl"`
	WinRate     float64 `json:"win_rate"`
	MaxDrawdown float64 `json:"max_drawdown"`
	SharpeRatio float64 `json:"sharpe_ratio"`
	Trades      int     `json:"trades"`
	Score       float64 `json:"score"` // combined metric for ranking
}

func RunOptimizer(cfg OptimizerConfig) []OptimizedConfig {
	if len(cfg.RSIPeriods) == 0 {
		cfg.RSIPeriods = []int{7, 14, 21}
	}
	if len(cfg.SMAWindows) == 0 {
		cfg.SMAWindows = []int{10, 20, 50, 100}
	}
	if len(cfg.AtrPeriods) == 0 {
		cfg.AtrPeriods = []int{7, 14, 21}
	}
	if cfg.Capital <= 0 {
		cfg.Capital = 1000
	}
	if cfg.Interval == "" {
		cfg.Interval = "1h"
	}

	// Fetch klines once
	client := NewBinanceClient()
	ctx, cancel := contextWithTimeout(30 * time.Second)
	klines, err := client.GetKlines(ctx, cfg.Symbol, cfg.Interval, 500)
	cancel()
	if err != nil {
		log.Printf("[OPTIMIZER] fetch failed: %v", err)
		return nil
	}

	if len(klines) < 100 {
		log.Printf("[OPTIMIZER] not enough data: %d", len(klines))
		return nil
	}

	results := make([]OptimizedConfig, 0)
	totalCombos := len(cfg.RSIPeriods) * len(cfg.SMAWindows) * len(cfg.AtrPeriods)
	count := 0

	for _, rsiP := range cfg.RSIPeriods {
		for _, smaW := range cfg.SMAWindows {
			for _, atrP := range cfg.AtrPeriods {
				count++

				btCfg := BacktestConfig{
					Symbol:         cfg.Symbol,
					Interval:       cfg.Interval,
					InitialCapital: cfg.Capital,
					Commission:     cfg.Commission,
					Slippage:       cfg.Slippage,
				}

				// Custom strategy using optimized parameters
				strategy := func(win []Kline, cp float64, p *CryptoPortfolio) *TradeSignal {
					return optimizedSignal(cfg.Symbol, win, p, rsiP, smaW, atrP)
				}

				result := RunBacktest(btCfg, klines, strategy)
				if result == nil || result.Trades == 0 {
					continue
				}

				// Score: weighted combination of metrics
				pnlScore := (result.TotalPnL / cfg.Capital) * 100 // %
				wrScore := result.WinRate
				ddPenalty := math.Max(0, result.MaxDrawdownPct) * 0.5
				tradeBonus := math.Min(float64(result.Trades)/50, 1) * 2

				score := pnlScore*0.4 + wrScore*0.3 - ddPenalty*0.2 + tradeBonus*0.1

				results = append(results, OptimizedConfig{
					RSIPeriod:   rsiP,
					SMAWindow:   smaW,
					ATRPeriod:   atrP,
					TotalPnL:    result.TotalPnL,
					WinRate:     result.WinRate,
					MaxDrawdown: result.MaxDrawdownPct,
					SharpeRatio: result.SharpeRatio,
					Trades:      result.Trades,
					Score:       math.Round(score*100) / 100,
				})

				if count%10 == 0 {
					log.Printf("[OPTIMIZER] %d/%d combos tested", count, totalCombos)
				}
			}
		}
	}

	// Sort by score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > 20 {
		results = results[:20]
	}

	log.Printf("[OPTIMIZER] %s: tested %d combinations, best score=%.2f (RSI=%d, SMA=%d, ATR=%d)",
		cfg.Symbol, totalCombos, results[0].Score, results[0].RSIPeriod, results[0].SMAWindow, results[0].ATRPeriod)

	return results
}

func optimizedSignal(symbol string, klines []Kline, portfolio *CryptoPortfolio, rsiPeriod, smaWindow, atrPeriod int) *TradeSignal {
	if len(klines) < smaWindow+10 {
		return &TradeSignal{Action: "HOLD", Symbol: symbol, Reason: "Not enough data"}
	}

	closes := closesFromKlines(klines)
	currentPrice := closes[len(closes)-1]

	rsi := calcRSI(closes, rsiPeriod)
	sma := avg(closes[len(closes)-smaWindow:])

	regime := DetectRegime(klines)
	signal := &TradeSignal{
		Symbol:     symbol,
		EntryPrice: currentPrice,
	}

	// Decision logic using optimized params
	switch {
	case rsi < 30 && currentPrice < sma*0.98:
		signal.Action = "BUY"
		signal.Reason = fmt.Sprintf("Optimized BUY: RSI(%d)=%.0f below SMA(%d)", rsiPeriod, rsi, smaWindow)
		signal.StopLoss = currentPrice * 0.96
		signal.TakeProfit = currentPrice * 1.06
		signal.PositionPct = 20
		signal.Confidence = 0.75

	case rsi > 70 && currentPrice > sma*1.02:
		signal.Action = "SELL"
		signal.Reason = fmt.Sprintf("Optimized SELL: RSI(%d)=%.0f above SMA(%d)", rsiPeriod, rsi, smaWindow)
		signal.StopLoss = currentPrice * 1.04
		signal.TakeProfit = currentPrice * 0.94
		signal.PositionPct = 15
		signal.Confidence = 0.7

	default:
		signal.Action = "HOLD"
		signal.Reason = fmt.Sprintf("RSI=%.0f near SMA=%.4f", rsi, sma)
		signal.PositionPct = 0
		signal.Confidence = 0.3
	}

	if regime.Regime == RegimeVolatile {
		signal.PositionPct *= 0.5
		signal.Confidence *= 0.8
	}

	signal.EntryPrice = math.Round(currentPrice*10000) / 10000
	signal.StopLoss = math.Round(signal.StopLoss*10000) / 10000
	signal.TakeProfit = math.Round(signal.TakeProfit*10000) / 10000
	signal.Confidence = math.Round(signal.Confidence*100) / 100

	return signal
}

// ──────────────────────────────────────────────────────────────────────
// Momentum Filter — only trade in the direction of the dominant trend
// ──────────────────────────────────────────────────────────────────────

type MomentumResult struct {
	Direction    string  `json:"direction"`    // UP, DOWN, SIDEWAYS
	Strength     float64 `json:"strength"`     // 0-1
	MACDValue    float64 `json:"macd"`
	MACDSignal   float64 `json:"macd_signal"`
	MACDHistogram float64 `json:"macd_histogram"`
	Recommendation string `json:"recommendation"`
}

func AnalyzeMomentum(klines []Kline) *MomentumResult {
	if len(klines) < 50 {
		return &MomentumResult{
			Direction: "SIDEWAYS", Strength: 0,
			Recommendation: "Not enough data",
		}
	}

	closes := closesFromKlines(klines)

	// MACD
	ema12 := ema(closes, 12)
	ema26 := ema(closes, 26)

	macdLine := make([]float64, len(closes))
	for i := range closes {
		if i < 25 {
			macdLine[i] = 0
			continue
		}
		macdLine[i] = ema12[i] - ema26[i]
	}
	signalLine := ema(macdLine, 9)

	macdV := macdLine[len(macdLine)-1]
	signalV := signalLine[len(signalLine)-1]
	histogram := macdV - signalV

	// Momentum strength
	roc := (closes[len(closes)-1] / closes[len(closes)-int(math.Min(14, float64(len(closes)-1)))]) - 1

	result := &MomentumResult{
		MACDValue:    math.Round(macdLine[len(macdLine)-1]*10000) / 10000,
		MACDSignal:   math.Round(signalLine[len(signalLine)-1]*10000) / 10000,
		MACDHistogram: math.Round(histogram*10000) / 10000,
	}

	switch {
	case histogram > 0 && roc > 0.01:
		result.Direction = "UP"
		result.Strength = math.Min(math.Abs(roc)*50, 1)
		result.Recommendation = "Strong bullish momentum — favor BUY setups"
	case histogram < 0 && roc < -0.01:
		result.Direction = "DOWN"
		result.Strength = math.Min(math.Abs(roc)*50, 1)
		result.Recommendation = "Strong bearish momentum — favor SELL setups"
	default:
		result.Direction = "SIDEWAYS"
		result.Strength = math.Min(math.Abs(roc)*30, 0.5)
		result.Recommendation = "Mixed momentum — grid strategy preferred"
	}

	result.Strength = math.Round(result.Strength*100) / 100
	return result
}

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}
