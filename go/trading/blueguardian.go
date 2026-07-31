package trading

import (
	"fmt"
	"sync"
	"time"
)

type BlueGuardianConfig struct {
	AccountBalance    float64
	DailyLossLimit    float64
	MaxDrawdown       float64
	ConsistencyLimit  float64
	MinTradingDays    int
	MinDayProfitPct   float64
	MaxDailyDrawdown  float64
	MaxPositions      int
}

type BlueGuardianState struct {
	mu           sync.Mutex
	DailyPNL     float64
	TotalPNL     float64
	PeakBalance  float64
	StartBalance float64
	TradingDays  int
	Wins         int
	Losses       int
	TotalTrades  int
	OpenTrades   int
	LastTradeDay time.Time
	Consistency  ConsistencyTracker
	IsPaused     bool
	PauseReason  string
}

type BlueGuardianBot struct {
	config BlueGuardianConfig
	state  *BlueGuardianState
	tracker *ConsistencyTracker
	strategy Strategy
}

func NewBlueGuardianBot(balance float64) *BlueGuardianBot {
	cfg := BlueGuardianConfig{
		AccountBalance:   balance,
		DailyLossLimit:   balance * 0.04,
		MaxDrawdown:      balance * 0.08,
		ConsistencyLimit: 15.0,
		MinTradingDays:   3,
		MinDayProfitPct:  0.005,
		MaxDailyDrawdown: balance * 0.04,
		MaxPositions:     6,
	}

	return &BlueGuardianBot{
		config:   cfg,
		state:    &BlueGuardianState{
			PeakBalance:  balance,
			StartBalance: balance,
		},
		tracker:  NewConsistencyTracker(),
		strategy: NewTimeBasedStrategy(),
	}
}

func (b *BlueGuardianBot) CanTrade() (bool, string) {
	b.state.mu.Lock()
	defer b.state.mu.Unlock()

	if b.state.IsPaused {
		return false, b.state.PauseReason
	}

	if b.state.OpenTrades >= b.config.MaxPositions {
		return false, fmt.Sprintf("max %d open positions reached", b.config.MaxPositions)
	}

	currentBalance := b.state.StartBalance + b.state.TotalPNL
	drawdown := ComputeDrawdown(currentBalance, b.state.PeakBalance)
	if drawdown >= b.config.MaxDrawdown {
		b.state.IsPaused = true
		b.state.PauseReason = fmt.Sprintf("max drawdown %.1f%% reached", drawdown)
		return false, b.state.PauseReason
	}

	if b.state.DailyPNL <= -b.config.DailyLossLimit {
		b.state.IsPaused = true
		b.state.PauseReason = "daily loss limit reached"
		return false, b.state.PauseReason
	}

	now := time.Now()
	if b.state.LastTradeDay.Day() == now.Day() && b.state.LastTradeDay.Month() == now.Month() {
		dayPNL := b.state.DailyPNL
		if dayPNL <= -b.config.MaxDailyDrawdown {
			return false, "daily drawdown exceeded"
		}
	}

	return true, ""
}

func (b *BlueGuardianBot) CanWithdraw() (bool, string) {
	b.state.mu.Lock()
	defer b.state.mu.Unlock()

	if b.state.TradingDays < b.config.MinTradingDays {
		return false, fmt.Sprintf("need %d trading days, have %d", b.config.MinTradingDays, b.state.TradingDays)
	}

	minProfit := b.state.StartBalance * b.config.MinDayProfitPct * float64(b.config.MinTradingDays)
	if b.state.TotalPNL < minProfit {
		return false, fmt.Sprintf("need $%.2f profit for withdrawal, have $%.2f", minProfit, b.state.TotalPNL)
	}

	if !b.tracker.CheckConsistency(b.config.ConsistencyLimit) {
		needed := b.tracker.ProfitNeededToPass(b.tracker.BestDay, b.config.ConsistencyLimit)
		return false, fmt.Sprintf("consistency rule: best day %.1f%% exceeds %.0f%% limit, need $%.2f more profit", b.tracker.BestDayPercent(), b.config.ConsistencyLimit, needed)
	}

	return true, ""
}

func (b *BlueGuardianBot) RecordTrade(won bool, pnl float64) {
	b.state.mu.Lock()
	defer b.state.mu.Unlock()

	b.state.TotalTrades++
	b.state.TotalPNL += pnl
	b.state.DailyPNL += pnl

	now := time.Now()
	if b.state.LastTradeDay.Day() != now.Day() || b.state.LastTradeDay.Month() != now.Month() {
		if b.state.LastTradeDay.Day() != 0 {
			b.state.TradingDays++
			b.tracker.RecordDay(b.state.DailyPNL)
		}
		b.state.DailyPNL = 0
	}
	b.state.LastTradeDay = now

	if won {
		b.state.Wins++
	} else {
		b.state.Losses++
	}

	currentBalance := b.state.StartBalance + b.state.TotalPNL
	if currentBalance > b.state.PeakBalance {
		b.state.PeakBalance = currentBalance
	}
}

func (b *BlueGuardianBot) OpenPosition() {
	b.state.mu.Lock()
	defer b.state.mu.Unlock()
	b.state.OpenTrades++
}

func (b *BlueGuardianBot) ClosePosition() {
	b.state.mu.Lock()
	defer b.state.mu.Unlock()
	if b.state.OpenTrades > 0 {
		b.state.OpenTrades--
	}
}

func (b *BlueGuardianBot) GetStatus() map[string]interface{} {
	b.state.mu.Lock()
	defer b.state.mu.Unlock()

	currentBalance := b.state.StartBalance + b.state.TotalPNL
	drawdown := ComputeDrawdown(currentBalance, b.state.PeakBalance)

	canTrade, reason := b.CanTrade()
	canWithdraw, wReason := b.CanWithdraw()

	winRate := 0.0
	if b.state.TotalTrades > 0 {
		winRate = float64(b.state.Wins) / float64(b.state.TotalTrades) * 100
	}

	return map[string]interface{}{
		"name":               "Blue Guardian",
		"account_balance":    b.config.AccountBalance,
		"current_balance":    currentBalance,
		"total_pnl":          b.state.TotalPNL,
		"daily_pnl":          b.state.DailyPNL,
		"peak_balance":       b.state.PeakBalance,
		"drawdown_pct":       drawdown,
		"max_drawdown_pct":   b.config.MaxDrawdown,
		"daily_loss_limit":   b.config.DailyLossLimit,
		"trades":             b.state.TotalTrades,
		"wins":               b.state.Wins,
		"losses":             b.state.Losses,
		"win_rate_pct":       winRate,
		"trading_days":       b.state.TradingDays,
		"min_trading_days":   b.config.MinTradingDays,
		"consistency_pct":    b.tracker.BestDayPercent(),
		"consistency_limit":  b.config.ConsistencyLimit,
		"can_trade":          canTrade,
		"can_withdraw":       canWithdraw,
		"pause_reason":       reason,
		"withdraw_reason":    wReason,
		"open_positions":     b.state.OpenTrades,
		"max_positions":      b.config.MaxPositions,
		"is_paused":          b.state.IsPaused,
	}
}

func (b *BlueGuardianBot) MaxPositionSize() float64 {
	return ComputePositionSize(b.config.AccountBalance, 0.5, 8, 0.0001)
}

func BlueGuardianDefaultConfig(balance float64) BlueGuardianConfig {
	return BlueGuardianConfig{
		AccountBalance:   balance,
		DailyLossLimit:   balance * 0.04,
		MaxDrawdown:      balance * 0.08,
		ConsistencyLimit: 15.0,
		MinTradingDays:   3,
		MinDayProfitPct:  0.005,
		MaxDailyDrawdown: balance * 0.04,
		MaxPositions:     4,
	}
}
