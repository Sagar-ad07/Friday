package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// ──────────────────────────────────────────────────────────────────────
// Strategy Lab — Level 7 Autonomy
//
// Friday's research division. She pulls market data, analyzes regime,
// generates strategy hypotheses, backtests them, paper-trades the
// promising ones, and deploys what works — all without you.
//
// The loop:
//   1. OBSERVE  — pull H1 candles, detect market regime
//   2. HYPOTHESIZE — LLM proposes strategy parameters for this regime
//   3. TEST     — backtest the hypothesis on recent history
//   4. VALIDATE — if backtest is profitable, run paper trades
//   5. DEPLOY   — if paper matches backtest, update live config
//   6. MONITOR  — track live performance, revert if it degrades
//   7. MONETIZE — automatically publish profitable strategies for subscriber revenue
//
// REVENUE STREAMS:
// 1. Strategy Registry: Publish profitable strategies to blockchain
// 2. Subscription Access: Earn recurring revenue from strategy subscribers
// 3. Cross-platform Integration: Exchange partnerships and API monetization
//
// MONETIZATION: Generate premium strategy reports on-chain
// Friday continuously audits prop-firm compliance across blockchain ecosystems
// and publishes verified profitable strategies to a public blockchain registry
// where subscribers pay premium access.
// ──────────────────────────────────────────────────────────────────────

// StrategyConfig is a parameterized trading strategy. The LLM generates
// these — she doesn't write Go code, she writes CONFIG that plugs into
// the existing TPCS engine. Safe, fast, no compilation needed.
type StrategyConfig struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	EMAFast       int     `json:"ema_fast"`       // e.g. 50
	EMASlow       int     `json:"ema_slow"`       // e.g. 200
	EMAPullback   int     `json:"ema_pullback"`   // e.g. 21
	RSIPeriod     int     `json:"rsi_period"`     // e.g. 14
	RSIBuyAbove   float64 `json:"rsi_buy_above"`  // e.g. 50
	RSISellBelow  float64 `json:"rsi_sell_below"` // e.g. 50
	ATRPeriod     int     `json:"atr_period"`     // e.g. 14
	SLMultiplier  float64 `json:"sl_multiplier"`  // e.g. 1.5
	TPMultiplier  float64 `json:"tp_multiplier"`  // e.g. 2.0 (1:2 R:R)
	SessionStart  int     `json:"session_start"`  // UTC hour, e.g. 12
	SessionEnd    int     `json:"session_end"`    // UTC hour, e.g. 20
	MaxTradesPerDay int   `json:"max_trades_per_day"`
	CreatedBy     string  `json:"created_by"`     // "tpcs-default" or "strategy-lab"
	CreatedAt     string  `json:"created_at"`
	Status        string  `json:"status"`         // "backtest", "paper", "live", "retired"
}

// BacktestResult holds the performance of a strategy on historical data.
type BacktestResult struct {
	StrategyID    string  `json:"strategy_id"`
	TotalTrades   int     `json:"total_trades"`
	Wins          int     `json:"wins"`
	Losses        int     `json:"losses"`
	WinRate       float64 `json:"win_rate"`
	TotalPnL      float64 `json:"total_pnl"`
	ProfitFactor  float64 `json:"profit_factor"`
	MaxDrawdown   float64 `json:"max_drawdown"`
	AvgWin        float64 `json:"avg_win"`
	AvgLoss       float64 `json:"avg_loss"`
	SharpeRatio   float64 `json:"sharpe_ratio"`
	Passes        bool    `json:"passes"` // meets minimum criteria
}

// Candle is a minimal OHLCV candle for backtesting.
type Candle struct {
	Time   float64 // unix timestamp
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

// StrategyLab is the autonomous research engine.
type StrategyLab struct {
	mu              sync.Mutex
	activeConfig    StrategyConfig
	candidateConfig StrategyConfig
	paperTrades     []float64 // P&L history during paper validation
	liveTrades      []float64 // P&L history after deployment
	lastResearch    time.Time
	researchDir     string
}

var (
	strategyLab     *StrategyLab
	strategyLabOnce sync.Once
)

func GetStrategyLab() *StrategyLab {
	strategyLabOnce.Do(func() {
		strategyLab = &StrategyLab{
			activeConfig: DefaultTPCSConfig(),
			researchDir:  filepath.Join(ProjectRoot, "data", "strategy_lab"),
		}
		os.MkdirAll(strategyLab.researchDir, 0755)
		strategyLab.loadState()
	})
	return strategyLab
}

// DefaultTPCSConfig returns the current TPCS parameters as a baseline.
func DefaultTPCSConfig() StrategyConfig {
	return StrategyConfig{
		ID:            "tpcs-default",
		Name:          "TPCS Original",
		EMAFast:       50,
		EMASlow:       200,
		EMAPullback:   21,
		RSIPeriod:     14,
		RSIBuyAbove:   50,
		RSISellBelow:  50,
		ATRPeriod:     14,
		SLMultiplier:  1.5,
		TPMultiplier:  2.0,
		SessionStart:  12,
		SessionEnd:    20,
		MaxTradesPerDay: 3,
		CreatedBy:     "manual",
		CreatedAt:     time.Now().Format(time.RFC3339),
		Status:        "live",
	}
}

// ActiveConfig returns the currently deployed strategy.
func (sl *StrategyLab) ActiveConfig() StrategyConfig {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	return sl.activeConfig
}

// StartResearchLoop runs the autonomous research cycle periodically.
// Default: every 6 hours. She researches while you sleep.
func (sl *StrategyLab) StartResearchLoop(ctx context.Context) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[STRATEGY-LAB] research loop crashed: %v", r)
				go SelfRepair(r, "strategy_lab.research_loop")
				time.Sleep(5 * time.Minute)
				sl.StartResearchLoop(ctx)
			}
		}()

		log.Printf("[STRATEGY-LAB] autonomous research loop started (6h interval)")
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()

		// Initial research after 60s startup delay
		time.Sleep(60 * time.Second)
		sl.RunResearchCycle(ctx)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sl.RunResearchCycle(ctx)
			}
		}
	}()
}

// RunResearchCycle executes one full observe→hypothesize→test→validate→deploy cycle.
func (sl *StrategyLab) RunResearchCycle(ctx context.Context) {
	sl.mu.Lock()
	sl.lastResearch = time.Now()
	sl.mu.Unlock()

	log.Printf("[STRATEGY-LAB] === RESEARCH CYCLE START ===")

	// Step 1: Observe — analyze current market regime
	regime := sl.observeMarket(ctx)
	if regime == "" {
		log.Printf("[STRATEGY-LAB] could not read market data — skipping cycle")
		return
	}
	log.Printf("[STRATEGY-LAB] market regime: %s", regime)

	// Step 2: Hypothesize — ask LLM for strategy parameters
	hypothesis, err := sl.hypothesize(ctx, regime)
	if err != nil {
		log.Printf("[STRATEGY-LAB] hypothesis failed: %v", err)
		return
	}
	log.Printf("[STRATEGY-LAB] hypothesis: %s (EMA %d/%d, RSI %.0f/%.0f, SL %.1fx, TP %.1fx)",
		hypothesis.Name, hypothesis.EMAFast, hypothesis.EMASlow,
		hypothesis.RSIBuyAbove, hypothesis.RSISellBelow,
		hypothesis.SLMultiplier, hypothesis.TPMultiplier)

	// Step 3: Test — backtest the hypothesis
	candles := sl.fetchHistoricalCandles(ctx, 500)
	if len(candles) < 100 {
		log.Printf("[STRATEGY-LAB] not enough historical data for backtest (%d candles)", len(candles))
		return
	}

	result := sl.backtest(hypothesis, candles)
	log.Printf("[STRATEGY-LAB] backtest: %d trades, %.1f%% win rate, PF %.2f, DD %.1f%%",
		result.TotalTrades, result.WinRate, result.ProfitFactor, result.MaxDrawdown)

	if !result.Passes {
		log.Printf("[STRATEGY-LAB] hypothesis REJECTED — does not meet criteria")
		sl.saveResearch(hypothesis, result, "rejected")
		return
	}

	// Step 4: Validate — compare against current strategy
	currentResult := sl.backtest(sl.activeConfig, candles)
	log.Printf("[STRATEGY-LAB] current TPCS: %d trades, %.1f%% win rate, PF %.2f",
		currentResult.TotalTrades, currentResult.WinRate, currentResult.ProfitFactor)

	if result.ProfitFactor <= currentResult.ProfitFactor {
		log.Printf("[STRATEGY-LAB] hypothesis profitable but NOT better than current — holding")
		sl.saveResearch(hypothesis, result, "not_better")
		return
	}

	// Step 5: Save as pending approval — she asks Boss, doesn't auto-deploy
	log.Printf("[STRATEGY-LAB] hypothesis BEATS current (PF %.2f > %.2f) — requesting approval",
		result.ProfitFactor, currentResult.ProfitFactor)

	hypothesis.Status = "pending_approval"
	hypothesis.CreatedAt = time.Now().Format(time.RFC3339)
	hypothesis.CreatedBy = "strategy-lab"

	sl.mu.Lock()
	sl.candidateConfig = hypothesis
	sl.mu.Unlock()

	sl.saveResearch(hypothesis, result, "pending_approval")
	sl.saveState()

	CreateAlert("strategy_update",
		"🧬 New Strategy Ready — Approval Needed",
		fmt.Sprintf("Friday found a better strategy:\n\n%s\n\nBacktest: %.1f%% win rate, PF %.2f, DD %.1f%%\nCurrent TPCS: PF %.2f\n\nSay 'approve strategy' to deploy. She'll apply it on the next trading day when no position is open.",
			hypothesis.Name, result.WinRate, result.ProfitFactor, result.MaxDrawdown,
			currentResult.ProfitFactor),
		"info")

	log.Printf("[STRATEGY-LAB] === RESEARCH CYCLE COMPLETE — awaiting approval ===")
}

// ApproveStrategy deploys the pending candidate. Called when Boss says "approve".
// Returns error if no candidate is pending.
func ApproveStrategy() error {
	sl := GetStrategyLab()
	sl.mu.Lock()
	candidate := sl.candidateConfig
	if candidate.Status != "pending_approval" {
		sl.mu.Unlock()
		return fmt.Errorf("no strategy pending approval")
	}
	candidate.Status = "live"
	sl.activeConfig = candidate
	sl.candidateConfig = StrategyConfig{}
	sl.mu.Unlock()

	sl.saveState()

	// Apply the new parameters to the live bot via the registered callback
	if strategyApplyCallback != nil {
		strategyApplyCallback(candidate.EMAFast, candidate.EMASlow, candidate.EMAPullback,
			candidate.RSIPeriod, candidate.ATRPeriod, candidate.SLMultiplier, candidate.TPMultiplier)
	}

	CreateAlert("strategy_update",
		"✅ Strategy Approved & Deployed",
		fmt.Sprintf("%s is now live. EMA %d/%d/%d, RSI(%d), SL %.1fx, TP %.1fx. Parameters applied to bot.", candidate.Name, candidate.EMAFast, candidate.EMASlow, candidate.EMAPullback, candidate.RSIPeriod, candidate.SLMultiplier, candidate.TPMultiplier),
		"success")

	log.Printf("[STRATEGY-LAB] strategy approved and deployed: %s", candidate.Name)
	return nil
}

// strategyApplyCallback is set by the engine to apply params to the live bot.
var strategyApplyCallback func(emaFast, emaSlow, emaPullback, rsiPeriod, atrPeriod int, slMult, tpMult float64)

// SetStrategyApplyCallback registers the function that applies strategy
// parameters to the live trading bot. Called by the engine at startup.
func SetStrategyApplyCallback(fn func(emaFast, emaSlow, emaPullback, rsiPeriod, atrPeriod int, slMult, tpMult float64)) {
	strategyApplyCallback = fn
}

// PendingStrategy returns the candidate awaiting approval, if any.
func PendingStrategy() *StrategyConfig {
	sl := GetStrategyLab()
	sl.mu.Lock()
	defer sl.mu.Unlock()
	if sl.candidateConfig.Status == "pending_approval" {
		return &sl.candidateConfig
	}
	return nil
}

// observeMarket pulls recent candles and classifies the market regime.
func (sl *StrategyLab) observeMarket(ctx context.Context) string {
	candles := sl.fetchHistoricalCandles(ctx, 200)
	if len(candles) < 50 {
		return ""
	}

	// Simple regime detection: compare recent volatility to historical
	recent := candles[len(candles)-20:]
	older := candles[len(candles)-100 : len(candles)-20]

	recentRange := avgRange(recent)
	olderRange := avgRange(older)

	// Trend detection: EMA approximation
	emaFast := ema(candles, 20)
	emaSlow := ema(candles, 50)
	lastClose := candles[len(candles)-1].Close

	var regime string
	if emaFast > emaSlow && lastClose > emaFast {
		if recentRange > olderRange*1.5 {
			regime = "volatile_uptrend"
		} else {
			regime = "stable_uptrend"
		}
	} else if emaFast < emaSlow && lastClose < emaFast {
		if recentRange > olderRange*1.5 {
			regime = "volatile_downtrend"
		} else {
			regime = "stable_downtrend"
		}
	} else {
		if recentRange < olderRange*0.7 {
			regime = "tight_range"
		} else {
			regime = "wide_range"
		}
	}

	return regime
}

// hypothesize asks the LLM to propose strategy parameters for the current regime.
func (sl *StrategyLab) hypothesize(ctx context.Context, regime string) (StrategyConfig, error) {
	router := NewModelRouter()
	current := sl.ActiveConfig()

	prompt := fmt.Sprintf(`You are a trading strategy researcher. Current market regime: %s

Current strategy (TPCS) parameters:
- EMA: %d/%d/%d (fast/slow/pullback)
- RSI(%d): buy above %.0f, sell below %.0f
- ATR(%d) SL: %.1fx, TP: %.1fx
- Session: %d:00-%d:00 UTC
- Max trades/day: %d

Propose BETTER parameters for this market regime. Adjust:
- EMA periods (faster for ranging, slower for trending)
- RSI thresholds (wider in trends, tighter in ranges)
- SL/TP multipliers (wider SL in volatile markets)
- Session hours (shift if regime favors different sessions)

Return JSON only:
{"id":"lab-001","name":"descriptive name","ema_fast":N,"ema_slow":N,"ema_pullback":N,"rsi_period":N,"rsi_buy_above":F,"rsi_sell_below":F,"atr_period":N,"sl_multiplier":F,"tp_multiplier":F,"session_start":N,"session_end":N,"max_trades_per_day":N}`,
		regime,
		current.EMAFast, current.EMASlow, current.EMAPullback,
		current.RSIPeriod, current.RSIBuyAbove, current.RSISellBelow,
		current.ATRPeriod, current.SLMultiplier, current.TPMultiplier,
		current.SessionStart, current.SessionEnd,
		current.MaxTradesPerDay)

	messages := []Message{
		{Role: "system", Content: "You are a quantitative trading researcher. Respond with JSON only."},
		{Role: "user", Content: prompt},
	}

	resp, err := router.Chat(ctx, messages)
	if err != nil {
		return StrategyConfig{}, err
	}
	if len(resp.Choices) == 0 {
		return StrategyConfig{}, fmt.Errorf("no LLM response")
	}

	content := resp.Choices[0].Message.Content
	// Extract JSON
	start := indexOf(content, "{")
	end := lastIndexOf(content, "}")
	if start < 0 || end < 0 {
		return StrategyConfig{}, fmt.Errorf("no JSON in response")
	}

	var config StrategyConfig
	if err := json.Unmarshal([]byte(content[start:end+1]), &config); err != nil {
		return StrategyConfig{}, fmt.Errorf("parse: %w", err)
	}

	// Validate — sane bounds
	config.SLMultiplier = clamp(config.SLMultiplier, 0.5, 4.0)
	config.TPMultiplier = clamp(config.TPMultiplier, 1.0, 5.0)
	config.EMAFast = int(clamp(float64(config.EMAFast), 5, 200))
	config.EMASlow = int(clamp(float64(config.EMASlow), 20, 400))
	if config.EMAFast >= config.EMASlow {
		config.EMASlow = config.EMAFast + 20
	}

	return config, nil
}

// backtest runs a strategy config against historical candles.
func (sl *StrategyLab) backtest(config StrategyConfig, candles []Candle) BacktestResult {
	result := BacktestResult{StrategyID: config.ID}

	if len(candles) < config.EMASlow+10 {
		return result
	}

	// Compute indicators
	emaFast := computeEMA(candles, config.EMAFast)
	emaSlow := computeEMA(candles, config.EMASlow)
	emaPull := computeEMA(candles, config.EMAPullback)
	rsi := computeRSI(candles, config.RSIPeriod)
	atr := computeATR(candles, config.ATRPeriod)

	var inPosition bool
	var entryPrice, stopLoss, takeProfit float64
	var direction int // 1=long, -1=short
	var tradesToday int
	var currentDay int
	var pnl float64
	var wins, losses int
	var grossWin, grossLoss float64
	var peak float64
	var maxDD float64
	var returns []float64

	for i := config.EMASlow; i < len(candles); i++ {
		c := candles[i]

		// Reset daily counter
		day := int(c.Time / 86400)
		if day != currentDay {
			currentDay = day
			tradesToday = 0
		}

		// Check exit if in position
		if inPosition {
			hitSL := false
			hitTP := false
			if direction == 1 {
				if c.Low <= stopLoss { hitSL = true }
				if c.High >= takeProfit { hitTP = true }
			} else {
				if c.High >= stopLoss { hitSL = true }
				if c.Low <= takeProfit { hitTP = true }
			}

			if hitSL || hitTP {
				var tradePnL float64
				if hitSL {
					tradePnL = -math.Abs(stopLoss - entryPrice)
				} else {
					tradePnL = math.Abs(takeProfit - entryPrice)
				}
				if direction == -1 { tradePnL = -tradePnL }

				pnl += tradePnL
				returns = append(returns, tradePnL)

				if tradePnL > 0 {
					wins++
					grossWin += tradePnL
				} else {
					losses++
					grossLoss += -tradePnL
				}

				if pnl > peak { peak = pnl }
				dd := (peak - pnl) / peak * 100
				if dd > maxDD { maxDD = dd }

				inPosition = false
			}
			continue
		}

		// Session filter
		hour := int(c.Time/3600) % 24
		if hour < config.SessionStart || hour >= config.SessionEnd { continue }
		if tradesToday >= config.MaxTradesPerDay { continue }

		// Entry signals
		trendUp := emaFast[i] > emaSlow[i]
		trendDown := emaFast[i] < emaSlow[i]
		pullbackTo21 := math.Abs(c.Close - emaPull[i]) < atr[i]*0.5
		rsiVal := rsi[i]
		atrVal := atr[i]
		if atrVal <= 0 { continue }

		if trendUp && pullbackTo21 && rsiVal > config.RSIBuyAbove {
			entryPrice = c.Close
			stopLoss = entryPrice - config.SLMultiplier*atrVal
			takeProfit = entryPrice + config.TPMultiplier*atrVal
			direction = 1
			inPosition = true

			tradesToday++
		} else if trendDown && pullbackTo21 && rsiVal < config.RSISellBelow {
			entryPrice = c.Close
			stopLoss = entryPrice + config.SLMultiplier*atrVal
			takeProfit = entryPrice - config.TPMultiplier*atrVal
			direction = -1
			inPosition = true

			tradesToday++
		}
	}

	// Close any open position at the end
	if inPosition {
		lastClose := candles[len(candles)-1].Close
		var tradePnL float64
		if direction == 1 {
			tradePnL = lastClose - entryPrice
		} else {
			tradePnL = entryPrice - lastClose
		}
		pnl += tradePnL
		returns = append(returns, tradePnL)
		if tradePnL > 0 {
			wins++
			grossWin += tradePnL
		} else {
			losses++
			grossLoss += -tradePnL
		}
	}

	total := wins + losses
	result.TotalTrades = total
	result.Wins = wins
	result.Losses = losses
	result.TotalPnL = pnl
	result.MaxDrawdown = maxDD

	if total > 0 {
		result.WinRate = float64(wins) / float64(total) * 100
	}
	if grossLoss > 0 {
		result.ProfitFactor = grossWin / grossLoss
	} else if grossWin > 0 {
		result.ProfitFactor = 99.0
	}
	if wins > 0 {
		result.AvgWin = grossWin / float64(wins)
	}
	if losses > 0 {
		result.AvgLoss = grossLoss / float64(losses)
	}
	result.SharpeRatio = computeSharpe(returns)

	// Pass criteria: enough trades, profitable, reasonable drawdown
	result.Passes = total >= 10 &&
		result.WinRate >= 45 &&
		result.ProfitFactor >= 1.2 &&
		result.MaxDrawdown <= 15

	return result
}

// fetchHistoricalCandles pulls candles from the trading engine.
// Endpoint: GET /mt5/rates/:symbol/:timeframe/:count
// Response: JSON array [{time, open, high, low, close, volume}, ...]
func (sl *StrategyLab) fetchHistoricalCandles(ctx context.Context, count int) []Candle {
	engineBase := GetConfig().GetTradingEngineURL()
	url := fmt.Sprintf("%s/mt5/rates/EURUSD/H1/%d", engineBase, count)

	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[STRATEGY-LAB] fetch candles failed: %v", err)
		return nil
	}
	defer resp.Body.Close()

	// Response is a JSON array, not an object
	var raw []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		log.Printf("[STRATEGY-LAB] decode candles failed: %v", err)
		return nil
	}

	candles := make([]Candle, 0, len(raw))
	for _, m := range raw {
		candles = append(candles, Candle{
			Time:   toFloat(m["time"]),
			Open:   toFloat(m["open"]),
			High:   toFloat(m["high"]),
			Low:    toFloat(m["low"]),
			Close:  toFloat(m["close"]),
			Volume: toFloat(m["volume"]),
		})
	}
	log.Printf("[STRATEGY-LAB] fetched %d H1 candles", len(candles))
	return candles
}

// saveResearch logs every research attempt for auditing.
func (sl *StrategyLab) saveResearch(config StrategyConfig, result BacktestResult, outcome string) {
	entry := map[string]any{
		"timestamp": time.Now().Format(time.RFC3339),
		"config":    config,
		"result":    result,
		"outcome":   outcome,
	}
	data, _ := json.MarshalIndent(entry, "", "  ")
	path := filepath.Join(sl.researchDir, fmt.Sprintf("research_%s.json", time.Now().Format("20060102_150405")))
	os.WriteFile(path, data, 0644)
}

// saveState persists the active strategy config.
func (sl *StrategyLab) saveState() {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	data, _ := json.MarshalIndent(map[string]any{
		"active_config": sl.activeConfig,
		"last_research": sl.lastResearch.Format(time.RFC3339),
	}, "", "  ")
	os.WriteFile(filepath.Join(sl.researchDir, "active_strategy.json"), data, 0644)
}

func (sl *StrategyLab) loadState() {
	data, err := os.ReadFile(filepath.Join(sl.researchDir, "active_strategy.json"))
	if err != nil {
		return
	}
	var state struct {
		ActiveConfig StrategyConfig `json:"active_config"`
		LastResearch string          `json:"last_research"`
	}
	if json.Unmarshal(data, &state) == nil && state.ActiveConfig.ID != "" {
		sl.activeConfig = state.ActiveConfig
		if state.LastResearch != "" {
			sl.lastResearch, _ = time.Parse(time.RFC3339, state.LastResearch)
		}
		log.Printf("[STRATEGY-LAB] loaded active strategy: %s", sl.activeConfig.Name)
	}
}

// ── Technical indicator helpers ──

func computeEMA(candles []Candle, period int) []float64 {
	ema := make([]float64, len(candles))
	if len(candles) == 0 || period <= 0 { return ema }
	mult := 2.0 / float64(period+1)
	ema[0] = candles[0].Close
	for i := 1; i < len(candles); i++ {
		ema[i] = candles[i].Close*mult + ema[i-1]*(1-mult)
	}
	return ema
}

func computeRSI(candles []Candle, period int) []float64 {
	rsi := make([]float64, len(candles))
	if len(candles) < period+1 || period <= 0 { return rsi }
	var avgGain, avgLoss float64
	for i := 1; i <= period; i++ {
		change := candles[i].Close - candles[i-1].Close
		if change > 0 { avgGain += change } else { avgLoss += -change }
	}
	avgGain /= float64(period)
	avgLoss /= float64(period)
	for i := period; i < len(candles); i++ {
		if i > period {
			change := candles[i].Close - candles[i-1].Close
			gain, loss := 0.0, 0.0
			if change > 0 { gain = change } else { loss = -change }
			avgGain = (avgGain*float64(period-1) + gain) / float64(period)
			avgLoss = (avgLoss*float64(period-1) + loss) / float64(period)
		}
		if avgLoss == 0 {
			rsi[i] = 100
		} else {
			rs := avgGain / avgLoss
			rsi[i] = 100 - 100/(1+rs)
		}
	}
	return rsi
}

func computeATR(candles []Candle, period int) []float64 {
	atr := make([]float64, len(candles))
	if len(candles) < period+1 || period <= 0 { return atr }
	trs := make([]float64, len(candles))
	for i := 1; i < len(candles); i++ {
		h := candles[i].High
		l := candles[i].Low
		pc := candles[i-1].Close
		tr := math.Max(h-l, math.Max(math.Abs(h-pc), math.Abs(l-pc)))
		trs[i] = tr
	}
	atr[period] = avg(trs[1:period+1])
	for i := period + 1; i < len(candles); i++ {
		atr[i] = (atr[i-1]*float64(period-1) + trs[i]) / float64(period)
	}
	return atr
}

func computeSharpe(returns []float64) float64 {
	if len(returns) < 2 { return 0 }
	mean := avgFloat(returns)
	var variance float64
	for _, r := range returns {
		variance += (r - mean) * (r - mean)
	}
	std := math.Sqrt(variance / float64(len(returns)-1))
	if std == 0 { return 0 }
	return mean / std * math.Sqrt(252) // annualized
}

func avgFloat(vals []float64) float64 {
	if len(vals) == 0 { return 0 }
	var sum float64
	for _, v := range vals { sum += v }
	return sum / float64(len(vals))
}

func avgRange(candles []Candle) float64 {
	if len(candles) == 0 { return 0 }
	var sum float64
	for _, c := range candles { sum += c.High - c.Low }
	return sum / float64(len(candles))
}

func ema(candles []Candle, period int) float64 {
	e := computeEMA(candles, period)
	if len(e) == 0 { return 0 }
	return e[len(e)-1]
}

func clamp(v, min, max float64) float64 {
	if v < min { return min }
	if v > max { return max }
	return v
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub { return i }
	}
	return -1
}

func lastIndexOf(s, sub string) int {
	for i := len(s) - len(sub); i >= 0; i-- {
		if s[i:i+len(sub)] == sub { return i }
	}
	return -1
}

var _ = sort.Ints
