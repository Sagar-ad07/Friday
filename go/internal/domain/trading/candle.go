package trading

import (
	"time"

	"github.com/friday-prototype/friday-go/pkg/util"
)

// Candle represents a price candle/bar
type Candle struct {
	Symbol    string
	Timeframe string
	OpenTime  time.Time
	CloseTime time.Time
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	Spread    float64
	Complete  bool
}

func (c *Candle) BodySize() float64 {
	if c.Close >= c.Open {
		return c.Close - c.Open
	}
	return c.Open - c.Close
}

func (c *Candle) UpperWick() float64 {
	return c.High - max(c.Open, c.Close)
}

func (c *Candle) LowerWick() float64 {
	return min(c.Open, c.Close) - c.Low
}

func (c *Candle) IsBullish() bool {
	return c.Close > c.Open
}

func (c *Candle) IsBearish() bool {
	return c.Close < c.Open
}

func (c *Candle) IsDoji(threshold float64) bool {
	return c.BodySize() <= (c.High-c.Low)*threshold
}

func (c *Candle) Range() float64 {
	return c.High - c.Low
}

func (c *Candle) MidPoint() float64 {
	return (c.High + c.Low) / 2
}

func (c *Candle) TypicalPrice() float64 {
	return (c.High + c.Low + c.Close) / 3
}

func (c *Candle) WeightedClose() float64 {
	return (c.High + c.Low + c.Close + c.Close) / 4
}

// Signal represents a trading signal from a strategy
type Signal struct {
	ID            string
	Symbol        string
	Timeframe     string
	Strategy      string
	Direction     Side
	Strength      float64 // 0-1
	Price         float64
	StopLoss      float64
	TakeProfit    float64
	Reason        string
	Indicators    map[string]float64
	Timestamp     time.Time
	ExpiresAt     *time.Time
	Metadata      map[string]any
}

func NewSignal(symbol, timeframe, strategy string, direction Side, price, sl, tp float64, strength float64, reason string) *Signal {
	return &Signal{
		ID:          util.GenerateIDWithPrefix("sig"),
		Symbol:      symbol,
		Timeframe:   timeframe,
		Strategy:    strategy,
		Direction:   direction,
		Strength:    strength,
		Price:       price,
		StopLoss:    sl,
		TakeProfit:  tp,
		Reason:      reason,
		Indicators:  make(map[string]float64),
		Timestamp:   time.Now().UTC(),
		Metadata:    make(map[string]any),
	}
}

func (s *Signal) IsValid() bool {
	return s.Strength > 0 && s.Price > 0 && s.StopLoss > 0 && s.TakeProfit > 0
}

func (s *Signal) RiskRewardRatio() float64 {
	risk := s.Price - s.StopLoss
	if s.Direction == SideSell {
		risk = s.StopLoss - s.Price
	}
	if risk <= 0 {
		return 0
	}
	reward := s.TakeProfit - s.Price
	if s.Direction == SideSell {
		reward = s.Price - s.TakeProfit
	}
	if reward <= 0 {
		return 0
	}
	return reward / risk
}

func (s *Signal) Expired() bool {
	if s.ExpiresAt == nil {
		return false
	}
	return time.Now().UTC().After(*s.ExpiresAt)
}