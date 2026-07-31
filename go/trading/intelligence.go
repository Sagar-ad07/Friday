package trading

import (
	"fmt"
	"math"
	"time"
)

type MarketRegime int

const (
	RegimeUnknown MarketRegime = iota
	RegimeTrendingBull
	RegimeTrendingBear
	RegimeRanging
	RegimeVolatile
	RegimeLowLiquidity
)

func (r MarketRegime) String() string {
	switch r {
	case RegimeTrendingBull:
		return "TRENDING_BULL"
	case RegimeTrendingBear:
		return "TRENDING_BEAR"
	case RegimeRanging:
		return "RANGING"
	case RegimeVolatile:
		return "VOLATILE"
	case RegimeLowLiquidity:
		return "LOW_LIQUIDITY"
	default:
		return "UNKNOWN"
	}
}

type MarketState struct {
	Regime        MarketRegime
	Confidence    float64
	Volatility    float64
	ATR           float64
	ADX           float64
	TrendStrength float64
	Support       float64
	Resistance    float64
	RSI           float64
	VolumeSpike   bool
	Timestamp     time.Time
}

type SignalFusion struct {
	Direction   string
	Confidence  float64
	EntryPrice  float64
	StopLoss    float64
	TakeProfit  float64
	Reasoning   string
	Confluences []string
	RiskScore   float64
	Regime      MarketRegime
	OptimalSize float64
}

type IntelligenceEngine struct {
	priceHistory  []float64
	volumeHistory []float64
	maxHistory    int
}

func NewIntelligenceEngine() *IntelligenceEngine {
	return &IntelligenceEngine{
		maxHistory: 500,
	}
}

func (ie *IntelligenceEngine) RecordPrice(price float64) {
	ie.priceHistory = append(ie.priceHistory, price)
	if len(ie.priceHistory) > ie.maxHistory {
		ie.priceHistory = ie.priceHistory[len(ie.priceHistory)-ie.maxHistory:]
	}
}

func (ie *IntelligenceEngine) RecordVolume(vol float64) {
	ie.volumeHistory = append(ie.volumeHistory, vol)
	if len(ie.volumeHistory) > ie.maxHistory {
		ie.volumeHistory = ie.volumeHistory[len(ie.volumeHistory)-ie.maxHistory:]
	}
}

func (ie *IntelligenceEngine) AnalyzeMarket() *MarketState {
	if len(ie.priceHistory) < 20 {
		return nil
	}

	state := &MarketState{
		Timestamp: time.Now(),
	}

	state.ATR = ie.calcATR(14)
	state.ADX = calcADX(ie.priceHistory, 14)
	state.Volatility = ie.calcVolatility(20)
	state.RSI = calcRSI(ie.priceHistory, 14)
	state.TrendStrength = ie.calcTrendStrength(20)
	state.Support = ie.calcSupport()
	state.Resistance = ie.calcResistance()
	state.Regime = ie.detectRegime(state)
	state.Confidence = ie.regimeConfidence(state)
	state.VolumeSpike = ie.detectVolumeSpike()

	return state
}

func (ie *IntelligenceEngine) FusionDecision(strategies []Strategy, market *MarketState) *SignalFusion {
	if market == nil || len(strategies) == 0 {
		return nil
	}

	if len(ie.priceHistory) < 2 {
		return nil
	}

	currentPrice := ie.priceHistory[len(ie.priceHistory)-1]

	var signals []*Signal
	for _, s := range strategies {
		sig := s.Analyze(currentPrice, ie.priceHistory)
		if sig != nil && sig.Confidence >= s.MinConfidence() {
			signals = append(signals, sig)
		}
	}

	if len(signals) == 0 {
		return nil
	}

	fusion := &SignalFusion{
		EntryPrice: currentPrice,
		Regime:     market.Regime,
	}

	buySignals := 0
	sellSignals := 0
	var buySL, sellSL, buyTP, sellTP float64
	var buyConf, sellConf float64

	for _, sig := range signals {
		switch sig.Direction {
		case "BUY":
			buySignals++
			buyConf += sig.Confidence
			buySL += sig.StopLoss
			buyTP += sig.TakeProfit
			fusion.Confluences = append(fusion.Confluences, sig.Reason)
		case "SELL":
			sellSignals++
			sellConf += sig.Confidence
			sellSL += sig.StopLoss
			sellTP += sig.TakeProfit
			fusion.Confluences = append(fusion.Confluences, sig.Reason)
		}
	}

	if buySignals > sellSignals {
		fusion.Direction = "BUY"
		fusion.Confidence = buyConf / float64(buySignals)
		fusion.StopLoss = buySL / float64(buySignals)
		fusion.TakeProfit = buyTP / float64(buySignals)
	} else if sellSignals > buySignals {
		fusion.Direction = "SELL"
		fusion.Confidence = sellConf / float64(sellSignals)
		fusion.StopLoss = sellSL / float64(sellSignals)
		fusion.TakeProfit = sellTP / float64(sellSignals)
	} else if buySignals == sellSignals && buySignals > 0 {
		if buyConf > sellConf {
			fusion.Direction = "BUY"
			fusion.Confidence = buyConf / float64(buySignals)
			fusion.StopLoss = buySL / float64(buySignals)
			fusion.TakeProfit = buyTP / float64(buySignals)
		} else {
			fusion.Direction = "SELL"
			fusion.Confidence = sellConf / float64(sellSignals)
			fusion.StopLoss = sellSL / float64(sellSignals)
			fusion.TakeProfit = sellTP / float64(sellSignals)
		}
	}

	fusion.RiskScore = ie.calcRiskScore(market, fusion)

	regimeMultiplier := 1.0
	switch market.Regime {
	case RegimeTrendingBull:
		if fusion.Direction == "BUY" {
			regimeMultiplier = 1.3
		} else {
			regimeMultiplier = 0.6
		}
	case RegimeTrendingBear:
		if fusion.Direction == "SELL" {
			regimeMultiplier = 1.3
		} else {
			regimeMultiplier = 0.6
		}
	case RegimeRanging:
		regimeMultiplier = 0.9
	case RegimeVolatile:
		regimeMultiplier = 0.5
	case RegimeLowLiquidity:
		regimeMultiplier = 0.3
	}

	fusion.Confidence = math.Min(fusion.Confidence*regimeMultiplier, 1.0)
	fusion.Reasoning = ie.buildReasoning(fusion, market, signals)

	baseSize := 0.01
	fusion.OptimalSize = math.Max(0.01, baseSize*(fusion.Confidence/0.5)*(1.0-math.Min(fusion.RiskScore, 1.0)))

	return fusion
}

func (ie *IntelligenceEngine) calcATR(period int) float64 {
	if len(ie.priceHistory) < period+1 {
		return 0
	}
	var sum float64
	start := len(ie.priceHistory) - period - 1
	for i := start + 1; i < len(ie.priceHistory); i++ {
		tr := math.Abs(ie.priceHistory[i] - ie.priceHistory[i-1])
		sum += tr
	}
	return sum / float64(period)
}

func (ie *IntelligenceEngine) calcVolatility(period int) float64 {
	if len(ie.priceHistory) < period {
		return 0
	}
	_, std := meanStd(ie.priceHistory, period)
	return std
}

func (ie *IntelligenceEngine) calcTrendStrength(period int) float64 {
	if len(ie.priceHistory) < period {
		return 0
	}
	start := len(ie.priceHistory) - period
	first := ie.priceHistory[start]
	last := ie.priceHistory[len(ie.priceHistory)-1]
	_, std := meanStd(ie.priceHistory, period)
	if std == 0 {
		return 0
	}
	return (last - first) / std
}

func (ie *IntelligenceEngine) calcSupport() float64 {
	if len(ie.priceHistory) < 20 {
		return 0
	}
	mins := 10
	step := len(ie.priceHistory) / mins
	if step < 1 {
		step = 1
	}
	var sum float64
	count := 0
	for i := 0; i < len(ie.priceHistory); i += step {
		end := i + step
		if end > len(ie.priceHistory) {
			end = len(ie.priceHistory)
		}
		min := ie.priceHistory[i]
		for j := i; j < end; j++ {
			if ie.priceHistory[j] < min {
				min = ie.priceHistory[j]
			}
		}
		sum += min
		count++
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func (ie *IntelligenceEngine) calcResistance() float64 {
	if len(ie.priceHistory) < 20 {
		return 0
	}
	mins := 10
	step := len(ie.priceHistory) / mins
	if step < 1 {
		step = 1
	}
	var sum float64
	count := 0
	for i := 0; i < len(ie.priceHistory); i += step {
		end := i + step
		if end > len(ie.priceHistory) {
			end = len(ie.priceHistory)
		}
		max := ie.priceHistory[i]
		for j := i; j < end; j++ {
			if ie.priceHistory[j] > max {
				max = ie.priceHistory[j]
			}
		}
		sum += max
		count++
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func (ie *IntelligenceEngine) detectRegime(state *MarketState) MarketRegime {
	if len(ie.priceHistory) < 50 {
		return RegimeRanging
	}

	rsi := state.RSI
	vol := state.Volatility
	trend := state.TrendStrength
	adx := state.ADX

	avgVol := ie.averageVolatility(50)
	if avgVol > 0 && vol/avgVol > 2.0 {
		return RegimeVolatile
	}

	// ADX > 20 = trending market. Use TrendStrength polarity for direction.
	if adx > 20 {
		if trend > 0 {
			return RegimeTrendingBull
		}
		return RegimeTrendingBear
	}

	// ADX <= 20 = ranging/choppy. Use RSI extremes for mean-reversion entry zones.
	if rsi > 30 && rsi < 70 && math.Abs(trend) < 0.3 {
		return RegimeRanging
	}

	isLowLiq := ie.detectLowLiquidity()
	if isLowLiq {
		return RegimeLowLiquidity
	}

	return RegimeRanging
}

func (ie *IntelligenceEngine) detectLowLiquidity() bool {
	if len(ie.priceHistory) < 10 {
		return false
	}
	recent := ie.priceHistory[len(ie.priceHistory)-10:]
	var changes float64
	for i := 1; i < len(recent); i++ {
		changes += math.Abs(recent[i] - recent[i-1])
	}
	avgChange := changes / float64(len(recent)-1)
	_, std := meanStd(ie.priceHistory, 50)
	return avgChange < std*0.1
}

func (ie *IntelligenceEngine) detectVolumeSpike() bool {
	if len(ie.volumeHistory) < 20 {
		return false
	}
	recent := ie.volumeHistory[len(ie.volumeHistory)-5:]
	older := ie.volumeHistory[:len(ie.volumeHistory)-5]
	var avgRecent, avgOlder float64
	for _, v := range recent {
		avgRecent += v
	}
	avgRecent /= float64(len(recent))
	for _, v := range older {
		avgOlder += v
	}
	avgOlder /= float64(len(older))
	return avgOlder > 0 && avgRecent/avgOlder > 1.5
}

func (ie *IntelligenceEngine) regimeConfidence(state *MarketState) float64 {
	switch state.Regime {
	case RegimeVolatile:
		return 0.6
	case RegimeTrendingBull, RegimeTrendingBear:
		return 0.8
	case RegimeRanging:
		return 0.7
	case RegimeLowLiquidity:
		return 0.4
	default:
		return 0.5
	}
}

func (ie *IntelligenceEngine) calcRiskScore(market *MarketState, fusion *SignalFusion) float64 {
	risk := 0.3

	if market.Volatility > 0.001 {
		risk += 0.2
	}
	if market.Regime == RegimeVolatile {
		risk += 0.2
	}
	if market.Regime == RegimeLowLiquidity {
		risk += 0.3
	}
	if len(fusion.Confluences) == 0 {
		risk += 0.1
	}
	if fusion.Confidence < 0.5 {
		risk += 0.2
	}

	return math.Min(risk, 1.0)
}

func (ie *IntelligenceEngine) buildReasoning(fusion *SignalFusion, market *MarketState, signals []*Signal) string {
	var reasoning string

	for _, c := range fusion.Confluences {
		reasoning += "Confluence: " + c + ". "
	}

	reasoning += "Regime: " + market.Regime.String() + ". "
	reasoning += fmt.Sprintf("Confidence: %.1f%%. ", fusion.Confidence*100)
	reasoning += fmt.Sprintf("Risk: %.1f%%. ", fusion.RiskScore*100)

	if fusion.Direction == "BUY" {
		reasoning += "Bullish bias with "
	} else {
		reasoning += "Bearish bias with "
	}
	reasoning += fmt.Sprintf("%d signal(s) out of available strategies.", len(signals))

	return reasoning
}

func (ie *IntelligenceEngine) averageVolatility(period int) float64 {
	if len(ie.priceHistory) < period {
		period = len(ie.priceHistory)
	}
	_, std := meanStd(ie.priceHistory, period)
	return std
}
