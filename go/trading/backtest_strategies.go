package trading

import (
	"log"
	"math"
	"sort"
	"strings"
	"time"
)

type StrategyResult struct {
	Name            string
	Trades          int
	Wins            int
	Losses          int
	WinRate         float64
	TotalProfit     float64
	MaxDrawdownPct  float64
	MaxConsecLosses int
	ProfitFactor    float64
	Expectancy      float64
	AvgWin          float64
	AvgLoss         float64
	Sharpe          float64
	TotalPips       float64
	TradeLog        []TradeRecord
	EquityCurve     []float64
}

type TradeRecord struct {
	EntryTime  time.Time
	ExitTime   time.Time
	Direction  string
	EntryPrice float64
	ExitPrice  float64
	SL         float64
	TP         float64
	Result     string // "win" or "loss"
	PnL        float64
	Cumulative float64
	Reason     string
}

func RunAllStrategies(candles []Candle, initialBalance float64) map[string]*StrategyResult {
	if len(candles) < 100 {
		return nil
	}

	// Apply jitter to break duplicate Yahoo prices (simulates real bid/ask spread)
	candles = AddJitter(candles)

	results := make(map[string]*StrategyResult)

	// 1. BB-RSI Mean Reversion
	results["BB-RSI Mean Reversion"] = runBBRSIBacktest(candles, initialBalance)
	results["London ORB Retest"] = runORBBacktest(candles, initialBalance)
	results["9-EMA Scalper"] = runEMAScalperBacktest(candles, initialBalance)

	return results
}

func runBBRSIBacktest(candles []Candle, balance float64) *StrategyResult {
	r := &StrategyResult{Name: "BB-RSI Mean Reversion"}
	strat := NewBBRsiStrategy()
	peak := balance
	maxDD := 0.0
	consLoss := 0
	maxConsLoss := 0
	totalWins := 0
	totalLosses := 0
	totalWinAmt := 0.0
	totalLossAmt := 0.0
	returns := []float64{}
	eqCurve := []float64{balance}

	for i := strat.Period + strat.RsiPeriod + 5; i < len(candles)-1; i++ {
		hist := candleCloseSlice(candles[:i+1])
		if len(hist) < 50 {
			continue
		}
		price := candles[i].Close
		signal := strat.Analyze(price, hist)
		if signal == nil || signal.Confidence < strat.MinConfidence() {
			eqCurve = append(eqCurve, peak)
			continue
		}

		sl := signal.StopLoss
		tp := signal.TakeProfit

		if signal.Direction == "BUY" {
			sl = price - (price-signal.StopLoss)*0.5
			tp = price + (signal.TakeProfit-price)*0.5
		} else {
			sl = price + (signal.StopLoss-price)*0.5
			tp = price - (price-signal.TakeProfit)*0.5
		}

		maxHold := 60
		var outcome string
		for j := i + 1; j < min(i+1+maxHold, len(candles)); j++ {
			c := candles[j]
			if signal.Direction == "BUY" {
				if c.Low <= sl {
					outcome = "loss"
					break
				}
				if c.High >= tp {
					outcome = "win"
					break
				}
			} else {
				if c.High >= sl {
					outcome = "loss"
					break
				}
				if c.Low <= tp {
					outcome = "win"
					break
				}
			}
		}
		if outcome == "" {
			outcome = "loss"
		}

		pnl := 0.0
		if outcome == "win" {
			pnl = 8.0
			totalWins++
			totalWinAmt += pnl
			consLoss = 0
		} else {
			pnl = -4.0
			totalLosses++
			totalLossAmt += math.Abs(pnl)
			consLoss++
			if consLoss > maxConsLoss {
				maxConsLoss = consLoss
			}
		}

		balance += pnl
		returns = append(returns, pnl)
		if balance > peak {
			peak = balance
		}
		dd := (peak - balance) / peak * 100
		if dd > maxDD {
			maxDD = dd
		}
		eqCurve = append(eqCurve, balance)
	}

	r = calcStats(r, totalWins, totalLosses, totalWinAmt, totalLossAmt, maxDD, maxConsLoss, returns, eqCurve)
	return r
}

func runORBBacktest(candles []Candle, balance float64) *StrategyResult {
	r := &StrategyResult{Name: "London ORB Retest"}
	peak := balance
	maxDD := 0.0
	consLoss := 0
	maxConsLoss := 0
	totalWins := 0
	totalLosses := 0
	totalWinAmt := 0.0
	totalLossAmt := 0.0
	returns := []float64{}
	eqCurve := []float64{balance}

	for i := 0; i < len(candles); i++ {
		c := candles[i]
		t := c.Time

		if t.Hour() != 7 || t.Minute() != 0 {
			continue
		}

		rangeEnd := i + 30
		if rangeEnd >= len(candles) {
			break
		}

		rangeHigh := candles[i].High
		rangeLow := candles[i].Low
		for j := i; j < rangeEnd; j++ {
			if candles[j].High > rangeHigh {
				rangeHigh = candles[j].High
			}
			if candles[j].Low < rangeLow {
				rangeLow = candles[j].Low
			}
		}
		rangePips := (rangeHigh - rangeLow) * 10000

		if rangePips < 4 {
			continue
		}

		tradeEnd := min(rangeEnd+90, len(candles))
		traded := false
		var direction string
		var entryPrice, entrySL, entryTP float64
		var entryTime time.Time

		for j := rangeEnd; j < tradeEnd-1; j++ {
			if traded {
				break
			}
			candle := candles[j]
			next := candles[j+1]

			if candle.Close > rangeHigh &&
				next.Low <= rangeHigh+0.0002 &&
				next.Low >= rangeHigh-0.001 {
				direction = "BUY"
				entryPrice = next.Close
				entrySL = entryPrice - 8*0.0001
				entryTP = entryPrice + 16*0.0001
				entryTime = next.Time
				traded = true
				break
			}

			if candle.Close < rangeLow &&
				next.High >= rangeLow-0.0002 &&
				next.High <= rangeLow+0.001 {
				direction = "SELL"
				entryPrice = next.Close
				entrySL = entryPrice + 8*0.0001
				entryTP = entryPrice - 16*0.0001
				entryTime = next.Time
				traded = true
				break
			}
		}

		if !traded {
			continue
		}

		outcome := "loss"
		maxHold := 360
		for j := rangeEnd; j < min(rangeEnd+maxHold, len(candles)); j++ {
			bar := candles[j]
			if timeBetween(entryTime, bar.Time) > 6*time.Hour {
				break
			}
			if direction == "BUY" {
				if bar.Low <= entrySL {
					outcome = "loss"
					break
				}
				if bar.High >= entryTP {
					outcome = "win"
					break
				}
			} else {
				if bar.High >= entrySL {
					outcome = "loss"
					break
				}
				if bar.Low <= entryTP {
					outcome = "win"
					break
				}
			}
		}

		pnl := 0.0
		if outcome == "win" {
			pnl = 16.0
			totalWins++
			totalWinAmt += pnl
			consLoss = 0
		} else {
			pnl = -8.0
			totalLosses++
			totalLossAmt += math.Abs(pnl)
			consLoss++
			if consLoss > maxConsLoss {
				maxConsLoss = consLoss
			}
		}

		balance += pnl
		returns = append(returns, pnl)
		if balance > peak {
			peak = balance
		}
		dd := (peak - balance) / peak * 100
		if dd > maxDD {
			maxDD = dd
		}
		eqCurve = append(eqCurve, balance)
	}

	r = calcStats(r, totalWins, totalLosses, totalWinAmt, totalLossAmt, maxDD, maxConsLoss, returns, eqCurve)
	return r
}

func runEMAScalperBacktest(candles []Candle, balance float64) *StrategyResult {
	r := &StrategyResult{Name: "9-EMA Scalper"}
	strat := NewScalpingStrategy()
	peak := balance
	maxDD := 0.0
	consLoss := 0
	maxConsLoss := 0
	totalWins := 0
	totalLosses := 0
	totalWinAmt := 0.0
	totalLossAmt := 0.0
	returns := []float64{}
	eqCurve := []float64{balance}
	excludeUntil := time.Time{}

	for i := strat.EmaPeriod + 3; i < len(candles)-1; i++ {
		hist := candleCloseSlice(candles[:i+1])
		if len(hist) < strat.EmaPeriod+2 {
			continue
		}
		if !excludeUntil.IsZero() && candles[i].Time.Before(excludeUntil) {
			continue
		}

		price := candles[i].Close
		signal := strat.Analyze(price, hist)
		if signal == nil || signal.Confidence < strat.MinConfidence() {
			continue
		}

		slPips := 12.0
		tpPips := 18.0
		sl := price - slPips*0.0001
		tp := price + tpPips*0.0001
		if signal.Direction == "SELL" {
			sl = price + slPips*0.0001
			tp = price - tpPips*0.0001
		}

		outcome := "loss"
		maxHold := 30
		for j := i + 1; j < min(i+1+maxHold, len(candles)); j++ {
			c := candles[j]
			if signal.Direction == "BUY" {
				if c.Low <= sl { outcome = "loss"; break }
				if c.High >= tp { outcome = "win"; break }
			} else {
				if c.High >= sl { outcome = "loss"; break }
				if c.Low <= tp { outcome = "win"; break }
			}
		}

		// Micro lot (0.01) PnL: $1 per pip
		pnl := -slPips
		if outcome == "win" {
			pnl = tpPips
			totalWins++
			totalWinAmt += pnl
			consLoss = 0
		} else {
			totalLosses++
			totalLossAmt += slPips
			consLoss++
			if consLoss > maxConsLoss {
				maxConsLoss = consLoss
			}
		}

		balance += pnl
		returns = append(returns, pnl)
		if balance > peak {
			peak = balance
		}
		dd := (peak - balance) / peak * 100
		if dd > maxDD {
			maxDD = dd
		}
		eqCurve = append(eqCurve, balance)
		excludeUntil = candles[i].Time.Add(15 * time.Minute)
	}

	r = calcStats(r, totalWins, totalLosses, totalWinAmt, totalLossAmt, maxDD, maxConsLoss, returns, eqCurve)
	return r
}

func calcStats(r *StrategyResult, wins, losses int, winAmt, lossAmt, maxDD float64, maxConsLoss int, returns, eqCurve []float64) *StrategyResult {
	r.Wins = wins
	r.Losses = losses
	r.Trades = wins + losses
	r.MaxDrawdownPct = maxDD
	r.MaxConsecLosses = maxConsLoss
	r.EquityCurve = eqCurve

	if r.Trades > 0 {
		r.WinRate = float64(wins) / float64(r.Trades) * 100
	}
	if wins > 0 {
		r.AvgWin = winAmt / float64(wins)
	}
	if losses > 0 {
		r.AvgLoss = lossAmt / float64(losses)
	}
	r.TotalProfit = winAmt - lossAmt
	if lossAmt > 0 {
		r.ProfitFactor = winAmt / lossAmt
	} else if winAmt > 0 {
		r.ProfitFactor = 999.99
	}
	r.Expectancy = (r.WinRate/100)*r.AvgWin - (1-r.WinRate/100)*math.Abs(r.AvgLoss)

	if len(returns) > 1 {
		avgRet := avgFloat(returns)
		var variance float64
		for _, v := range returns {
			variance += (v - avgRet) * (v - avgRet)
		}
		variance /= float64(len(returns))
		stdDev := math.Sqrt(variance)
		if stdDev > 0 {
			r.Sharpe = (avgRet / stdDev) * math.Sqrt(252)
		}
	}

	return r
}

func candleCloseSlice(candles []Candle) []float64 {
	p := make([]float64, len(candles))
	for i, c := range candles {
		p[i] = c.Close
	}
	return p
}

func timeBetween(a, b time.Time) time.Duration {
	if b.After(a) {
		return b.Sub(a)
	}
	return a.Sub(b)
}

func avgFloat(s []float64) float64 {
	if len(s) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range s {
		sum += v
	}
	return sum / float64(len(s))
}

func PrintStrategyResults(results map[string]*StrategyResult, initialBalance float64) {
	keys := make([]string, 0, len(results))
	for k := range results {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	sep := strings.Repeat("=", 72)

	for _, name := range keys {
		r := results[name]
		if r == nil || r.Trades == 0 {
			continue
		}

		log.Println(sep)
		log.Printf("  %s", strings.ToUpper(name))
		log.Println(sep)
		log.Printf("  Trades:              %d", r.Trades)
		log.Printf("  Wins:                %d  (%.1f%%)", r.Wins, r.WinRate)
		log.Printf("  Losses:              %d  (%.1f%%)", r.Losses, 100-r.WinRate)
		log.Printf("  Total Profit:        $%.2f", r.TotalProfit)
		log.Printf("  Final Balance:       $%.2f  (from $%.0f)", initialBalance+r.TotalProfit, initialBalance)
		log.Printf("  Return:              %.1f%%", (r.TotalProfit/initialBalance)*100)
		log.Printf("  Avg Win:             $%.2f", r.AvgWin)
		log.Printf("  Avg Loss:            $%.2f", r.AvgLoss)
		log.Printf("  Profit Factor:       %.2f", r.ProfitFactor)
		log.Printf("  Expectancy:          $%.2f/trade", r.Expectancy)
		log.Printf("  Max Drawdown:        %.2f%%", r.MaxDrawdownPct)
		log.Printf("  Max Consec. Losses:  %d", r.MaxConsecLosses)
		log.Printf("  Sharpe Ratio:        %.2f", r.Sharpe)
		log.Println(sep)
	}

	log.Println()
	log.Println(strings.Repeat("=", 72))
	log.Println("  STRATEGY COMPARISON")
	log.Println(strings.Repeat("=", 72))
	log.Printf("  %-25s %8s %8s %8s %12s %12s", "Strategy", "Trades", "WinRate", "ProfitF", "Total PnL", "Max DD")
	log.Println(strings.Repeat("-", 72))

	best := ""
	bestPnL := -1e9
	for _, name := range keys {
		r := results[name]
		if r == nil || r.Trades == 0 {
			continue
		}
		log.Printf("  %-25s %8d %7.1f%% %7.2f %10.2f %10.2f%%",
			name, r.Trades, r.WinRate, r.ProfitFactor, r.TotalProfit, r.MaxDrawdownPct)
		if r.TotalProfit > bestPnL {
			bestPnL = r.TotalProfit
			best = name
		}
	}
	log.Println(strings.Repeat("-", 72))
	log.Printf("  BEST STRATEGY: %s (PnL: $%.2f)", best, bestPnL)
	log.Println(strings.Repeat("=", 72))
}
