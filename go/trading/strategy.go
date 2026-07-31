package trading

import (
	"math"
	"time"
)

type Signal struct {
	Direction string  // "BUY" or "SELL"
	Confidence float64
	EntryPrice float64
	StopLoss   float64
	TakeProfit float64
	Reason     string
}

type Strategy interface {
	Name() string
	Analyze(price float64, hist []float64) *Signal
	Timeframe() string
	MinConfidence() float64
}

type BBRsiStrategy struct {
	Period     int
	StdDev     float64
	RsiPeriod  int
	Oversold   float64
	Overbought float64
}

func NewBBRsiStrategy() *BBRsiStrategy {
	return &BBRsiStrategy{
		Period:     14,
		StdDev:     1.5,
		RsiPeriod:  9,
		Oversold:   45,
		Overbought: 55,
	}
}

func (s *BBRsiStrategy) Name() string { return "BB-RSI Mean Reversion" }

func (s *BBRsiStrategy) Timeframe() string { return "M1" }

func (s *BBRsiStrategy) MinConfidence() float64 { return 0.70 }

func (s *BBRsiStrategy) Analyze(price float64, hist []float64) *Signal {
	if len(hist) < s.Period {
		return nil
	}

	mean, std := meanStd(hist, s.Period)
	rsi := calcRSI(hist, s.RsiPeriod)

	upper := mean + s.StdDev*std
	lower := mean - s.StdDev*std

	if rsi < s.Oversold && price <= lower {
		return &Signal{
			Direction:  "BUY",
			Confidence: 0.75 + (s.Oversold-rsi)/100.0,
			EntryPrice: price,
			StopLoss:   price - std*1.5,
			TakeProfit: price + std*2.0,
			Reason:     "Oversold + lower BB touch",
		}
	}

	if rsi > s.Overbought && price >= upper {
		return &Signal{
			Direction:  "SELL",
			Confidence: 0.75 + (rsi-s.Overbought)/100.0,
			EntryPrice: price,
			StopLoss:   price + std*1.5,
			TakeProfit: price - std*2.0,
			Reason:     "Overbought + upper BB touch",
		}
	}

	return nil
}

type LondonOrbStrategy struct {
	RangeHigh  float64
	RangeLow   float64
	RangePips  float64
}

func NewLondonOrbStrategy() *LondonOrbStrategy {
	return &LondonOrbStrategy{}
}

func (s *LondonOrbStrategy) Name() string { return "London ORB Retest" }

func (s *LondonOrbStrategy) Timeframe() string { return "M5" }

func (s *LondonOrbStrategy) MinConfidence() float64 { return 0.80 }

func (s *LondonOrbStrategy) Analyze(price float64, hist []float64) *Signal {
	if s.RangeHigh == 0 || s.RangeLow == 0 {
		return nil
	}

	retestZone := s.RangePips * 0.0001

	if price > s.RangeHigh && price < s.RangeHigh+retestZone {
		return &Signal{
			Direction:  "BUY",
			Confidence: 0.85,
			EntryPrice: price,
			StopLoss:   s.RangeLow - retestZone,
			TakeProfit: price + s.RangePips*2*0.0001,
			Reason:     "Bullish breakout + retest of OR high",
		}
	}

	if price < s.RangeLow && price > s.RangeLow-retestZone {
		return &Signal{
			Direction:  "SELL",
			Confidence: 0.85,
			EntryPrice: price,
			StopLoss:   s.RangeHigh + retestZone,
			TakeProfit: price - s.RangePips*2*0.0001,
			Reason:     "Bearish breakdown + retest of OR low",
		}
	}

	return nil
}

type ScalpingStrategy struct {
	EmaPeriod  int
	Threshold  float64
}

func NewScalpingStrategy() *ScalpingStrategy {
	return &ScalpingStrategy{
		EmaPeriod: 9,
		Threshold: 0.0001,
	}
}

func (s *ScalpingStrategy) Name() string { return "9-EMA Scalper" }

func (s *ScalpingStrategy) Timeframe() string { return "M1" }

func (s *ScalpingStrategy) MinConfidence() float64 { return 0.65 }

func (s *ScalpingStrategy) Analyze(price float64, hist []float64) *Signal {
	if len(hist) < s.EmaPeriod+2 {
		return nil
	}

	ema := calcEMA(hist, s.EmaPeriod)
	prevEma := calcEMA(hist[:len(hist)-1], s.EmaPeriod)
	prevPrice := hist[len(hist)-2]

	diff := price - ema
	prevDiff := prevPrice - prevEma

	if diff > s.Threshold && prevDiff <= 0 {
		return &Signal{
			Direction:  "BUY",
			Confidence: 0.70,
			EntryPrice: price,
			StopLoss:   price - s.Threshold*5,
			TakeProfit: price + s.Threshold*10,
			Reason:     "Price crossed above EMA",
		}
	}

	if diff < -s.Threshold && prevDiff >= 0 {
		return &Signal{
			Direction:  "SELL",
			Confidence: 0.70,
			EntryPrice: price,
			StopLoss:   price + s.Threshold*5,
			TakeProfit: price - s.Threshold*10,
			Reason:     "Price crossed below EMA",
		}
	}

	return nil
}

func meanStd(data []float64, period int) (float64, float64) {
	if len(data) < period {
		period = len(data)
	}
	start := len(data) - period
	var sum float64
	for i := start; i < len(data); i++ {
		sum += data[i]
	}
	mean := sum / float64(period)

	var sqSum float64
	for i := start; i < len(data); i++ {
		d := data[i] - mean
		sqSum += d * d
	}
	std := math.Sqrt(sqSum / float64(period))
	return mean, std
}

func calcRSI(data []float64, period int) float64 {
	if len(data) < period+1 {
		return 50
	}
	start := len(data) - period - 1
	var gains, losses float64
	for i := start + 1; i < len(data); i++ {
		diff := data[i] - data[i-1]
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

func calcEMA(data []float64, period int) float64 {
	if len(data) < period {
		period = len(data)
	}
	multiplier := 2.0 / float64(period+1)
	start := len(data) - period
	var ema float64
	for i := start; i < len(data); i++ {
		if i == start {
			ema = data[i]
		} else {
			ema = (data[i]-ema)*multiplier + ema
		}
	}
	return ema
}

type TimeBasedStrategy struct {
	strategies []Strategy
}

// NewTimeBasedStrategy now uses the SINGLE best-documented high-win-rate
// strategy the user requested ("best proven one, run after that only"):
// Trend Pullback Continuation (EMA50/200 + EMA21 pullback + RSI confirm).
// The legacy BB-RSI / EMA-9 Scalper / London-ORB strats stay in this file
// for reference but are no longer wired into the rotation. Time-based
// session selection is now handled internally by TPCS itself.
func NewTimeBasedStrategy() *TimeBasedStrategy {
	return &TimeBasedStrategy{
		strategies: []Strategy{
			NewTrendPullbackContinuationStrategy(),
		},
	}
}

func (t *TimeBasedStrategy) GetActiveStrategy(hour int) Strategy {
	if len(t.strategies) == 0 {
		return nil
	}
	return t.strategies[0] // single strategy — no time-of-day rotation needed
}

func (t *TimeBasedStrategy) AllStrategies() []Strategy {
	return t.strategies
}

// GetTPCS returns the Trend Pullback Continuation strategy for parameter updates.
func (t *TimeBasedStrategy) GetTPCS() *TrendPullbackContinuationStrategy {
	for _, s := range t.strategies {
		if tpcs, ok := s.(*TrendPullbackContinuationStrategy); ok {
			return tpcs
		}
	}
	return nil
}

func (t *TimeBasedStrategy) Analyze(price float64, hist []float64) *Signal {
	hour := time.Now().Hour()
	return t.GetActiveStrategy(hour).Analyze(price, hist)
}

func (t *TimeBasedStrategy) Name() string {
	return "Time-Based (" + t.GetActiveStrategy(time.Now().Hour()).Name() + ")"
}

func (t *TimeBasedStrategy) Timeframe() string {
	return t.GetActiveStrategy(time.Now().Hour()).Timeframe()
}

func (t *TimeBasedStrategy) MinConfidence() float64 {
	return t.GetActiveStrategy(time.Now().Hour()).MinConfidence()
}

type ConsistencyTracker struct {
	DailyProfits []float64
	TotalProfit  float64
	BestDay      float64
}

func NewConsistencyTracker() *ConsistencyTracker {
	return &ConsistencyTracker{}
}

func (c *ConsistencyTracker) RecordDay(profit float64) {
	c.DailyProfits = append(c.DailyProfits, profit)
	c.TotalProfit += profit
	if profit > c.BestDay {
		c.BestDay = profit
	}
}

func (c *ConsistencyTracker) BestDayPercent() float64 {
	if c.TotalProfit <= 0 {
		return 0
	}
	return (c.BestDay / c.TotalProfit) * 100
}

func (c *ConsistencyTracker) CheckConsistency(limitPct float64) bool {
	if c.TotalProfit <= 0 || c.BestDay == 0 {
		return true
	}
	return c.BestDayPercent() <= limitPct
}

func (c *ConsistencyTracker) ProfitNeededToPass(bestDay, limitPct float64) float64 {
	if bestDay == 0 {
		return 0
	}
	needed := bestDay / (limitPct / 100.0)
	shortfall := needed - c.TotalProfit
	if shortfall < 0 {
		return 0
	}
	return shortfall
}

func ComputePositionSize(accountBalance, riskPct, stopLossPips float64, pipValue float64) float64 {
	riskAmount := accountBalance * (riskPct / 100.0)
	riskPerUnit := stopLossPips * pipValue
	if riskPerUnit <= 0 {
		return 0.01
	}
	size := riskAmount / riskPerUnit
	size = math.Round(size*100) / 100
	if size < 0.01 {
		return 0.01
	}
	return size
}

func ComputeDrawdown(currentBalance, peakBalance float64) float64 {
	if peakBalance <= 0 {
		return 0
	}
	return ((peakBalance - currentBalance) / peakBalance) * 100
}

func IsNewsTime(t time.Time) bool {
	hour := t.Hour()
	day := t.Weekday()

	if day == time.Saturday || day == time.Sunday {
		return false
	}

	newsHours := []struct {
		start, end int
	}{
		{8, 9},
		{12, 13},
		{14, 15},
	}

	for _, nh := range newsHours {
		if hour >= nh.start && hour < nh.end {
			return true
		}
	}
	return false
}

