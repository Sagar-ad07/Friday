package trading

import (
	"context"
)

// StrategyType defines the type of trading strategy
type StrategyType string

const (
	StrategyLondonORB    StrategyType = "london_orb"
	StrategyBollingerRSI StrategyType = "bollinger_rsi"
	StrategyBreakout     StrategyType = "breakout"
	StrategyMeanReversion StrategyType = "mean_reversion"
	StrategyTrendFollowing StrategyType = "trend_following"
)

// Strategy defines the interface for trading strategies
type Strategy interface {
	Name() string
	Type() StrategyType
	Symbol() string
	Timeframe() string

	// Initialize sets up the strategy
	Initialize(ctx context.Context, account *Account, config map[string]any) error

	// OnCandle processes a new completed candle
	OnCandle(ctx context.Context, candle *Candle) (*Signal, error)

	// OnTick processes a price tick (for real-time strategies)
	OnTick(ctx context.Context, price float64, volume float64) (*Signal, error)

	// CheckEntry checks if entry conditions are met
	CheckEntry(ctx context.Context, candle *Candle) (*Signal, error)

	// CheckExit checks if exit conditions are met for a position
	CheckExit(ctx context.Context, position *Position, candle *Candle) (bool, string)

	// GetParameters returns strategy parameters
	GetParameters() map[string]any

	// SetParameters updates strategy parameters
	SetParameters(params map[string]any) error

	// Validate checks if strategy is properly configured
	Validate() error

	// Reset resets strategy state
	Reset()

	// IsActive returns whether strategy is active
	IsActive() bool

	// SetActive enables/disables the strategy
	SetActive(active bool)
}

// BaseStrategy provides common functionality
type BaseStrategy struct {
	name       string
	strategyType StrategyType
	symbol     string
	timeframe  string
	active     bool
	parameters map[string]any
}

func NewBaseStrategy(name string, st StrategyType, symbol, timeframe string) *BaseStrategy {
	return &BaseStrategy{
		name:         name,
		strategyType: st,
		symbol:       symbol,
		timeframe:    timeframe,
		active:       true,
		parameters:   make(map[string]any),
	}
}

func (b *BaseStrategy) Name() string          { return b.name }
func (b *BaseStrategy) Type() StrategyType    { return b.strategyType }
func (b *BaseStrategy) Symbol() string        { return b.symbol }
func (b *BaseStrategy) Timeframe() string     { return b.timeframe }
func (b *BaseStrategy) IsActive() bool        { return b.active }
func (b *BaseStrategy) SetActive(a bool)      { b.active = a }
func (b *BaseStrategy) GetParameters() map[string]any { return b.parameters }
func (b *BaseStrategy) SetParameters(p map[string]any) error {
	b.parameters = p
	return nil
}
func (b *BaseStrategy) Reset()                {}
func (b *BaseStrategy) Validate() error       { return nil }
func (b *BaseStrategy) Initialize(ctx context.Context, account *Account, config map[string]any) error { return nil }
func (b *BaseStrategy) OnCandle(ctx context.Context, candle *Candle) (*Signal, error) { return nil, nil }
func (b *BaseStrategy) OnTick(ctx context.Context, price, volume float64) (*Signal, error) { return nil, nil }
func (b *BaseStrategy) CheckEntry(ctx context.Context, candle *Candle) (*Signal, error) { return nil, nil }
func (b *BaseStrategy) CheckExit(ctx context.Context, position *Position, candle *Candle) (bool, string) { return false, "" }