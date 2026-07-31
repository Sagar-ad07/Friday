package trading

import (
	"fmt"
	"log"
	"math"
	"sort"
	"time"
)

// ──────────────────────────────────────────────────────────────────────
// Backtesting Engine — test any strategy on historical data
// ──────────────────────────────────────────────────────────────────────

type BacktestConfig struct {
	Symbol         string    `json:"symbol"`
	Interval       string    `json:"interval"`   // 1m, 5m, 15m, 1h, 4h, 1d
	InitialCapital float64   `json:"initial_capital"`
	StartTime      time.Time `json:"start_time"`
	EndTime        time.Time `json:"end_time"`
	Commission     float64   `json:"commission"`  // % per trade (e.g., 0.001 for 0.1%)
	Slippage       float64   `json:"slippage"`    // % per trade
}

type BacktestTrade struct {
	EntryTime   time.Time `json:"entry_time"`
	ExitTime    time.Time `json:"exit_time"`
	Side        string    `json:"side"`
	EntryPrice  float64   `json:"entry_price"`
	ExitPrice   float64   `json:"exit_price"`
	Quantity    float64   `json:"quantity"`
	PnL         float64   `json:"pnl"`
	PnLPercent  float64   `json:"pnl_percent"`
	ExitReason  string    `json:"exit_reason"`
}

type BacktestResult struct {
	Config         BacktestConfig  `json:"config"`
	TotalPnL       float64         `json:"total_pnl"`
	TotalPnLPercent float64        `json:"total_pnl_percent"`
	Trades         int             `json:"trades"`
	Wins           int             `json:"wins"`
	Losses         int             `json:"losses"`
	WinRate        float64         `json:"win_rate"`
	MaxDrawdown    float64         `json:"max_drawdown"`
	MaxDrawdownPct float64         `json:"max_drawdown_pct"`
	SharpeRatio    float64         `json:"sharpe_ratio"`
	ProfitFactor   float64         `json:"profit_factor"`
	AvgWin         float64         `json:"avg_win"`
	AvgLoss        float64         `json:"avg_loss"`
	AvgTradePct    float64         `json:"avg_trade_pct"`
	TradeLog       []BacktestTrade `json:"trade_log"`
	EquityCurve    []float64       `json:"equity_curve"`
	RegimeBreakdown map[string]int `json:"regime_breakdown"`
}

// StrategyFunc is a pluggable strategy: given klines up to now, returns signal
type StrategyFunc func(klines []Kline, currentPrice float64, portfolio *CryptoPortfolio) *TradeSignal

// RunBacktest runs a strategy on historical kline data
func RunBacktest(cfg BacktestConfig, klines []Kline, strategy StrategyFunc) *BacktestResult {
	if len(klines) < 50 {
		return &BacktestResult{
			Config: cfg,
			TotalPnL: cfg.InitialCapital,
			Trades:  0,
		}
	}

	portfolio := NewCryptoPortfolio(cfg.InitialCapital)
	equity := cfg.InitialCapital
	peakEquity := equity
	maxDD := 0.0
	maxDDPct := 0.0

	result := &BacktestResult{
		Config:          cfg,
		TradeLog:        make([]BacktestTrade, 0),
		EquityCurve:     make([]float64, 0),
		RegimeBreakdown: make(map[string]int),
	}

	totalWins := 0
	totalLosses := 0
	totalWinAmount := 0.0
	totalLossAmount := 0.0
	returns := make([]float64, 0)

	openTrade := (*BacktestTrade)(nil)
	currentPrices := make(map[string]float64)

	for i := 50; i < len(klines); i++ {
		window := klines[:i+1]
		currentPrice := klines[i].Close
		currentPrices[cfg.Symbol] = currentPrice

		// Get signal from strategy
		signal := strategy(window, currentPrice, portfolio)

		// Close open trade if signal reversed or HOLD
		if openTrade != nil {
			shouldClose := signal == nil || signal.Action == "HOLD" ||
				(signal.Action != openTrade.Side)

			if shouldClose {
				// Apply slippage
				exitPrice := currentPrice * (1 + cfg.Slippage/100)
				if openTrade.Side == "SELL" {
					exitPrice = currentPrice * (1 - cfg.Slippage/100)
				}
				// Apply commission
				commission := openTrade.Quantity * exitPrice * cfg.Commission / 100

				pnl := 0.0
				if openTrade.Side == "BUY" {
					pnl = (exitPrice - openTrade.EntryPrice) * openTrade.Quantity
				} else {
					pnl = (openTrade.EntryPrice - exitPrice) * openTrade.Quantity
				}
				pnl -= commission

				openTrade.PnL = math.Round(pnl*10000) / 10000
				openTrade.PnLPercent = math.Round((pnl/(openTrade.EntryPrice*openTrade.Quantity))*10000) / 100
				openTrade.ExitPrice = math.Round(exitPrice*10000) / 10000
				openTrade.ExitTime = time.Unix(int64(klines[i].CloseTime/1000), 0)
				openTrade.ExitReason = signalReason(signal)

				equity += pnl
				returns = append(returns, pnl)

				if pnl > 0 {
					totalWins++
					totalWinAmount += pnl
				} else {
					totalLosses++
					totalLossAmount += math.Abs(pnl)
				}

				result.TradeLog = append(result.TradeLog, *openTrade)
				openTrade = nil
			}
		}

		// Open new trade
		if signal != nil && signal.Action != "HOLD" && openTrade == nil {
			positionPct := signal.PositionPct / 100
			if positionPct <= 0 {
				positionPct = 0.1 // default 10%
			}

			entryPrice := signal.EntryPrice * (1 + cfg.Slippage/100)
			if signal.Action == "SELL" {
				entryPrice = signal.EntryPrice * (1 - cfg.Slippage/100)
			}

			qty := (equity * positionPct) / entryPrice
			commission := qty * entryPrice * cfg.Commission / 100
			equity -= commission

			openTrade = &BacktestTrade{
				EntryTime:  time.Unix(int64(klines[i].OpenTime/1000), 0),
				Side:       signal.Action,
				EntryPrice: math.Round(entryPrice*10000) / 10000,
				Quantity:   math.Round(qty*100000) / 100000,
			}
		}

		// Track equity curve
		currentEquity := portfolio.GetEquity(currentPrices)
		if currentEquity > peakEquity {
			peakEquity = currentEquity
		}
		dd := peakEquity - currentEquity
		ddPct := (dd / peakEquity) * 100
		if ddPct > maxDDPct {
			maxDD = dd
			maxDDPct = ddPct
		}
		result.EquityCurve = append(result.EquityCurve, math.Round(currentEquity*100)/100)

		// Track regime breakdown
		regime := DetectRegime(window)
		result.RegimeBreakdown[string(regime.Regime)]++
	}

	// Close any remaining position
	if openTrade != nil {
		finalPrice := klines[len(klines)-1].Close
		openTrade.ExitPrice = finalPrice
		openTrade.ExitTime = time.Unix(int64(klines[len(klines)-1].CloseTime/1000), 0)
		openTrade.ExitReason = "end of backtest"

		pnl := 0.0
		if openTrade.Side == "BUY" {
			pnl = (finalPrice - openTrade.EntryPrice) * openTrade.Quantity
		} else {
			pnl = (openTrade.EntryPrice - finalPrice) * openTrade.Quantity
		}
		openTrade.PnL = math.Round(pnl*10000) / 10000
		openTrade.PnLPercent = math.Round((pnl/(openTrade.EntryPrice*openTrade.Quantity))*10000) / 100

		equity += pnl
		if pnl > 0 {
			totalWins++
			totalWinAmount += pnl
		} else {
			totalLosses++
			totalLossAmount += math.Abs(pnl)
		}
		result.TradeLog = append(result.TradeLog, *openTrade)
	}

	// Calculate metrics
	result.TotalPnL = math.Round((equity-cfg.InitialCapital)*100) / 100
	result.TotalPnLPercent = math.Round(((equity-cfg.InitialCapital)/cfg.InitialCapital)*10000) / 100
	result.Trades = len(result.TradeLog)
	result.Wins = totalWins
	result.Losses = totalLosses
	result.WinRate = 0
	if result.Trades > 0 {
		result.WinRate = math.Round(float64(totalWins)/float64(result.Trades)*10000) / 100
	}
	result.MaxDrawdown = math.Round(maxDD*100) / 100
	result.MaxDrawdownPct = math.Round(maxDDPct*100) / 100
	result.AvgWin = 0
	if totalWins > 0 {
		result.AvgWin = math.Round(totalWinAmount/float64(totalWins)*100) / 100
	}
	result.AvgLoss = 0
	if totalLosses > 0 {
		result.AvgLoss = math.Round(totalLossAmount/float64(totalLosses)*100) / 100
	}
	result.AvgTradePct = 0
	if result.Trades > 0 {
		result.AvgTradePct = math.Round((result.TotalPnLPercent/float64(result.Trades))*100) / 100
	}

	// Profit factor
	if totalLossAmount > 0 {
		result.ProfitFactor = math.Round(totalWinAmount/totalLossAmount*100) / 100
	} else if totalWinAmount > 0 {
		result.ProfitFactor = 999.99
	} else {
		result.ProfitFactor = 0
	}

	// Sharpe ratio (simplified: using avg return / std dev of returns)
	if len(returns) > 1 {
		avgReturn := avg(returns)
		variance := 0.0
		for _, r := range returns {
			variance += (r - avgReturn) * (r - avgReturn)
		}
		variance /= float64(len(returns))
		stdDev := math.Sqrt(variance)
		if stdDev > 0 {
			// Annualized Sharpe (assuming ~252 trading days for daily data)
			result.SharpeRatio = math.Round((avgReturn/stdDev)*math.Sqrt(252)*100) / 100
		}
	}

	return result
}

// ──────────────────────────────────────────────────────────────────────
// Pre-built Strategy: Adaptive RSI + Trend
// ──────────────────────────────────────────────────────────────────────

func AdaptiveStrategy(klines []Kline, currentPrice float64, portfolio *CryptoPortfolio) *TradeSignal {
	if len(klines) < 50 {
		return &TradeSignal{Action: "HOLD", Reason: "Not enough data"}
	}

	// Generate signal using the existing GenerateSignal
	symbol := "BTCUSDT"
	signal := GenerateSignal(symbol, klines, portfolio)

	// Adaptive: adjust position size based on volatility
	regime := DetectRegime(klines)
	if regime.Regime == RegimeVolatile {
		signal.PositionPct = signal.PositionPct * 0.5 // halve position in volatility
	}

	return signal
}

// ──────────────────────────────────────────────────────────────────────
// Pre-built Strategy: Mean Reversion
// ──────────────────────────────────────────────────────────────────────

func MeanReversionStrategy(klines []Kline, currentPrice float64, portfolio *CryptoPortfolio) *TradeSignal {
	if len(klines) < 50 {
		return &TradeSignal{Action: "HOLD", Reason: "Not enough data"}
	}

	closes := closesFromKlines(klines)
	rsi := calcRSI(closes, 14)
	bbUpper, bbLower := bollingerBands(closes, 20, 2)

	symbol := "BTCUSDT"
	signal := &TradeSignal{
		Symbol:     symbol,
		EntryPrice: currentPrice,
	}

	switch {
	case currentPrice <= bbLower && rsi < 30:
		signal.Action = "BUY"
		signal.Reason = fmt.Sprintf("Mean reversion BUY: price below lower BB (RSI=%.0f)", rsi)
		signal.StopLoss = currentPrice * 0.97
		signal.TakeProfit = avg(closes[len(closes)-20:]) * 1.01
		signal.PositionPct = 15
		signal.Confidence = 0.7

	case currentPrice >= bbUpper && rsi > 70:
		signal.Action = "SELL"
		signal.Reason = fmt.Sprintf("Mean reversion SELL: price above upper BB (RSI=%.0f)", rsi)
		signal.StopLoss = currentPrice * 1.03
		signal.TakeProfit = avg(closes[len(closes)-20:]) * 0.99
		signal.PositionPct = 12
		signal.Confidence = 0.65

	default:
		signal.Action = "HOLD"
		signal.Reason = "Price within Bollinger Bands"
		signal.PositionPct = 0
		signal.Confidence = 0.3
	}

	return signal
}

// Bollinger Bands
func bollingerBands(prices []float64, period int, stdDev float64) (upper, lower float64) {
	if len(prices) < period {
		return 0, 0
	}
	m := avg(prices[len(prices)-period:])
	variance := 0.0
	for i := len(prices) - period; i < len(prices); i++ {
		variance += (prices[i] - m) * (prices[i] - m)
	}
	variance /= float64(period)
	s := math.Sqrt(variance)
	return m + s*stdDev, m - s*stdDev
}

func signalReason(signal *TradeSignal) string {
	if signal == nil {
		return "no signal"
	}
	return signal.Reason
}

// ──────────────────────────────────────────────────────────────────────
// Compare strategies side by side
// ──────────────────────────────────────────────────────────────────────

type StrategyComparison struct {
	Name   string           `json:"name"`
	Result *BacktestResult  `json:"result"`
}

func CompareStrategies(cfg BacktestConfig, klines []Kline, strategies map[string]StrategyFunc) []StrategyComparison {
	results := make([]StrategyComparison, 0)
	keys := make([]string, 0, len(strategies))
	for k := range strategies {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, name := range keys {
		strategy := strategies[name]
		result := RunBacktest(cfg, klines, strategy)
		results = append(results, StrategyComparison{
			Name:   name,
			Result: result,
		})
	}

	return results
}

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}
