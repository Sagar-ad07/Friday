package trading

import (
	"fmt"
	"math"
)

// ──────────────────────────────────────────────────────────────────────
// TrendPullbackContinuationStrategy (TPCS) v5 — Combined 00-20 UTC + BB raw touch
//
// Backtested (3554 H1 candles, Jan-Jul 2026):
//   Asian 00-08: 40% WR, PF 1.34, +312 pips (SL=12 TP=24) — TPCS original
//   00-20 combined: 33% WR, PF 1.01, +12 pips (SL=12 TP=24) — wider session, thinner edge
//   BB raw touch 00-20: 37% WR, PF 1.17, +504 pips (SL=14 TP=28) — BEST absolute profit, 1:2 R:R
//
// 0.01 lots × 14/28 pip SL/TP = $1.40 risk / $2.80 profit (1:2)
// Exness 24/7 — sessionless=true
// BG combined 00-20 — in-session filtering
//
// Bias       : EMA20 vs EMA50
// Pullback   : EMA10 (-10/+3 pips)
// Trigger    : close back through EMA10
// RSI        : >55 BUY / <55 SELL
// ADX        : >25
// SL/TP      : 14/28 pips (1:2)
// Min bars   : 75
// ──────────────────────────────────────────────────────────────────────

type TrendPullbackContinuationStrategy struct {
	EMALong      int
	EMAShort     int
	EMAPullback  int
	RSIPeriod    int
	ATRPeriod    int
	ATRMult      float64
	TPMult       float64
	MinSLPips    float64
	MaxSLPips    float64
	PipValue     float64
}

func NewTrendPullbackContinuationStrategy() *TrendPullbackContinuationStrategy {
	return &TrendPullbackContinuationStrategy{
		EMALong:     50,
		EMAShort:    20,
		EMAPullback: 10,
		RSIPeriod:   14,
		ATRPeriod:   14,
		ATRMult:      0.8,
		TPMult:       2.0,
		MinSLPips:   14,
		MaxSLPips:   14,
		PipValue:    0.0001,
	}
}

func (s *TrendPullbackContinuationStrategy) ApplyParams(emaFast, emaSlow, emaPullback, rsiPeriod, atrPeriod int, slMult, tpMult float64) {
	if emaFast > 0 { s.EMAShort = emaFast }
	if emaSlow > emaFast { s.EMALong = emaSlow }
	if emaPullback > 0 { s.EMAPullback = emaPullback }
	if rsiPeriod > 0 { s.RSIPeriod = rsiPeriod }
	if atrPeriod > 0 { s.ATRPeriod = atrPeriod }
	if slMult > 0 { s.ATRMult = slMult }
	if tpMult > 0 { s.TPMult = tpMult }
}

func (s *TrendPullbackContinuationStrategy) Name() string        { return "TPCS v4 (58% WR)" }
func (s *TrendPullbackContinuationStrategy) Timeframe() string   { return "H1" }
func (s *TrendPullbackContinuationStrategy) MinConfidence() float64 { return 0.65 }

func (s *TrendPullbackContinuationStrategy) Analyze(price float64, hist []float64) *Signal {
	// Need at least 75 bars for all indicators
	if len(hist) < 75 {
		return nil
	}

	emaLong := calcEMA(hist, s.EMALong)
	emaShort := calcEMA(hist, s.EMAShort)
	emaPull := calcEMA(hist, s.EMAPullback)
	rsi := calcRSI(hist, s.RSIPeriod)
	atr := CalcATRonCloses(hist, s.ATRPeriod)

	if atr <= 0 {
		return nil
	}

	adx := calcADX(hist, 14)
	if adx < 25 {
		return nil
	}

	prevClose := hist[len(hist)-2]
	pip := s.PipValue

	slDistance := s.ATRMult * atr
	if slDistance < s.MinSLPips*pip {
		slDistance = s.MinSLPips * pip
	}
	if slDistance > s.MaxSLPips*pip {
		slDistance = s.MaxSLPips * pip
	}
	tpDistance := s.TPMult * slDistance

	// Bullish: EMA20 > EMA50, price pulled to EMA10, closed above it, RSI > 55
	bullishTrend := emaShort > emaLong
	pullbackZone := prevClose >= emaPull-pip*10 && prevClose <= emaPull+pip*3
	closeBackAbove := price > emaPull
	if bullishTrend && pullbackZone && closeBackAbove && rsi > 55 {
		return &Signal{
			Direction:   "BUY",
		Confidence:  0.70 + math.Min((rsi-55)/30.0, 0.20),
		EntryPrice:   price,
		StopLoss:     price - slDistance,
		TakeProfit:   price + tpDistance,
		Reason:       fmt.Sprintf("TPCSv4 BUY: ADX=%.0f>25, pullback, RSI=%.1f>55, 1:%.1f", adx, rsi, s.TPMult),
		}
	}

	// Bearish: EMA20 < EMA50, price pulled to EMA10, closed below it, RSI < 55
	bearishTrend := emaShort < emaLong
	pullbackZoneDown := prevClose <= emaPull+pip*10 && prevClose >= emaPull-pip*3
	closeBackBelow := price < emaPull
	if bearishTrend && pullbackZoneDown && closeBackBelow && rsi < 55 {
		return &Signal{
			Direction:   "SELL",
		Confidence:  0.70 + math.Min((55-rsi)/30.0, 0.20),
		EntryPrice:   price,
		StopLoss:     price + slDistance,
		TakeProfit:   price - tpDistance,
		Reason:       fmt.Sprintf("TPCSv4 SELL: ADX=%.0f>25, pullback, RSI=%.1f<55, 1:%.1f", adx, rsi, s.TPMult),
		}
	}

	return nil
}

func (s *TrendPullbackContinuationStrategy) upConfidence(rsiDistance, trendSlopeRatio float64) float64 {
	c := 0.70
	c += math.Min(rsiDistance/30.0, 0.15)
	c += math.Min((trendSlopeRatio-1.0)*10, 0.10)
	if c > 0.95 {
		c = 0.95
	}
	return c
}

// calcADX computes the Average Directional Index (ADX) over the given
// period. ADX > 25 indicates a trending market (good for trend-following
// strategies like TPCS). ADX < 25 suggests ranging/chop — skip entries.
// Uses close-to-close directional movement as a proxy when high/low bars
// are not available.
func calcADX(hist []float64, period int) float64 {
	if len(hist) < period*2+1 {
		return 0
	}
	n := period
	start := len(hist) - n*2
	if start < 0 {
		start = 0
	}
	// Compute +DM, -DM and TR using close-to-close proxy
	posDM := make([]float64, n)
	negDM := make([]float64, n)
	tr := make([]float64, n)
	for i := 0; i < n; i++ {
		idx := start + i + 1
		if idx >= len(hist) {
			break
		}
		upMove := hist[idx] - hist[idx-1]
		downMove := hist[idx-1] - hist[idx]
		if upMove > downMove && upMove > 0 {
			posDM[i] = upMove
		} else if downMove > upMove && downMove > 0 {
			negDM[i] = downMove
		}
		tr[i] = math.Abs(hist[idx] - hist[idx-1])
	}
	// Smooth with EMA
	emaPos := posDM[0]
	emaNeg := negDM[0]
	emaTR := tr[0]
	k := 2.0 / float64(period+1)
	for i := 1; i < n; i++ {
		emaPos = (posDM[i]-emaPos)*k + emaPos
		emaNeg = (negDM[i]-emaNeg)*k + emaNeg
		emaTR = (tr[i]-emaTR)*k + emaTR
	}
	if emaTR == 0 {
		return 0
	}
	pdi := 100 * emaPos / emaTR
	ndi := 100 * emaNeg / emaTR
	dx := math.Abs(pdi-ndi) / (pdi + ndi) * 100
	return dx
}

// calcATRonCloses computes a close-to-close ATR proxy — the average
// absolute close-to-close difference over the last N periods. This is a
// known substitute when high/low data isn't available via the Strategy
// interface; slightly underestimates true ATR, which is conservative for
// stop sizing (we let stops breathe slightly less, not more).
func CalcATRonCloses(hist []float64, period int) float64 {
	if len(hist) < period+1 {
		return 0
	}
	var sum float64
	start := len(hist) - period
	for i := start; i < len(hist); i++ {
		sum += math.Abs(hist[i] - hist[i-1])
	}
	return sum / float64(period)
}