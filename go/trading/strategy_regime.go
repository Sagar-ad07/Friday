package trading

import (
	"fmt"
)

// ──────────────────────────────────────────────────────────────────────
// RegimeBasedStrategy — auto-switches between strategies based on
// live market regime detected by IntelligenceEngine.
//
//   TRENDING_BULL/BEAR → TPCS (trend pullback continuation)
//   RANGING            → BB-RSI Mean Reversion on H1
//   VOLATILE           → Stay flat (wide spreads, unpredictable)
//   LOW_LIQUIDITY      → Stay flat (Asian session)
// ──────────────────────────────────────────────────────────────────────

type RegimeBasedStrategy struct {
	market     *MarketState
	tpcs       *TrendPullbackContinuationStrategy
	meanRevH1  *MeanReversionH1Strategy
}

func NewRegimeBasedStrategy() *RegimeBasedStrategy {
	return &RegimeBasedStrategy{
		tpcs:      NewTrendPullbackContinuationStrategy(),
		meanRevH1: NewMeanReversionH1Strategy(),
	}
}

func (r *RegimeBasedStrategy) SetMarket(market *MarketState) {
	r.market = market
}

func (r *RegimeBasedStrategy) ActiveStrategy() Strategy {
	if r.market == nil {
		return r.tpcs
	}
	switch r.market.Regime {
	case RegimeTrendingBull, RegimeTrendingBear:
		return r.tpcs
	case RegimeRanging:
		return r.meanRevH1
	case RegimeVolatile, RegimeLowLiquidity:
		return nil
	default:
		return r.tpcs
	}
}

func (r *RegimeBasedStrategy) ActiveName() string {
	s := r.ActiveStrategy()
	if s == nil {
		return "FLAT (" + r.market.Regime.String() + ")"
	}
	return s.Name()
}

func (r *RegimeBasedStrategy) CurrentRegime() string {
	if r.market == nil {
		return "UNKNOWN"
	}
	return r.market.Regime.String()
}

func (r *RegimeBasedStrategy) AllStrategies() []Strategy {
	return []Strategy{r.tpcs, r.meanRevH1}
}

func (r *RegimeBasedStrategy) GetTPCS() *TrendPullbackContinuationStrategy {
	return r.tpcs
}

// Analyze delegates to the active strategy for the current regime.
func (r *RegimeBasedStrategy) Analyze(price float64, hist []float64) *Signal {
	s := r.ActiveStrategy()
	if s == nil {
		return nil // flat — no signals in volatile/illiquid
	}
	return s.Analyze(price, hist)
}

func (r *RegimeBasedStrategy) Name() string {
	return "Regime-Based (" + r.ActiveName() + ")"
}

func (r *RegimeBasedStrategy) Timeframe() string { return "H1" }

func (r *RegimeBasedStrategy) MinConfidence() float64 {
	s := r.ActiveStrategy()
	if s == nil {
		return 1.0
	}
	return s.MinConfidence()
}

// ──────────────────────────────────────────────────────────────────────
// MeanReversionH1Strategy — BB-RSI mean reversion on H1 timeframe.
//
// Uses Bollinger Bands (20,2) + RSI(14) to catch price extremes in
// ranging markets. This is the counter-trend strategy that TPCS can't
// execute — when ADX < 20 and market is ranging, mean reversion
// outperforms trend-following.
// ──────────────────────────────────────────────────────────────────────

type MeanReversionH1Strategy struct {
	Period     int
	StdDev     float64
	RsiPeriod  int
	Oversold   float64
	Overbought float64
}

func NewMeanReversionH1Strategy() *MeanReversionH1Strategy {
	return &MeanReversionH1Strategy{
		Period:     20,
		StdDev:     2.0,
		RsiPeriod:  14,
		Oversold:   35,
		Overbought: 65,
	}
}

func (s *MeanReversionH1Strategy) Name() string {
	return "BB-RSI Mean Reversion (H1)"
}

func (s *MeanReversionH1Strategy) Timeframe() string { return "H1" }

func (s *MeanReversionH1Strategy) MinConfidence() float64 { return 0.70 }

func (s *MeanReversionH1Strategy) Analyze(price float64, hist []float64) *Signal {
	if len(hist) < s.Period {
		return nil
	}

	mean, std := meanStd(hist, s.Period)
	rsi := calcRSI(hist, s.RsiPeriod)

	upper := mean + s.StdDev*std
	lower := mean - s.StdDev*std

	// Only trade when price is at extreme band AND RSI confirms
	if rsi < s.Oversold && price <= lower {
		slDist := std * 1.5
		tpDist := std * 3.0 // 1:2 R:R
		return &Signal{
			Direction:  "BUY",
			Confidence: 0.70 + (s.Oversold-rsi)/100.0,
			EntryPrice: price,
			StopLoss:   price - slDist,
			TakeProfit: price + tpDist,
			Reason:     fmt.Sprintf("Ranging mean-reversion: oversold(%.0f) + lower BB touch", rsi),
		}
	}

	if rsi > s.Overbought && price >= upper {
		slDist := std * 1.5
		tpDist := std * 3.0
		return &Signal{
			Direction:  "SELL",
			Confidence: 0.70 + (rsi-s.Overbought)/100.0,
			EntryPrice: price,
			StopLoss:   price + slDist,
			TakeProfit: price - tpDist,
			Reason:     fmt.Sprintf("Ranging mean-reversion: overbought(%.0f) + upper BB touch", rsi),
		}
	}

	return nil
}
