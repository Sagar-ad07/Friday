package safety

import (
	"sync"
	"time"
)

// TradingGuardians enforces safety protocols for the money machine
type TradingGuardians struct {
	limits SafetyLimits
	state  *SafetyState
	mutex  sync.RWMutex
}

// SafetyLimits defines hard safety boundaries
type SafetyLimits struct {
	MaxDailyLoss      float64       // $150 max loss
	MaxPositionSize   float64       // 10% of account
	MinConfidence     float64       // 85% AI confidence minimum
	CooldownAfterLoss time.Duration // 5min cooldown after loss
	MaxTradesPerHour  int           // Maximum trades per hour
	MaxSpread         float64       // Maximum spread in pips

	// User-specified rules
	MinConsistency   float64  // 15% minimum consistency (win rate)
	MaxDrawdown      float64  // Maximum drawdown percentage
	MinHoldingTime   int      // Minimum holding time in seconds
}

// SafetyState tracks current safety state
type SafetyState struct {
	DailyLoss        float64
	LastLossTime     time.Time
	TradesToday      int
	TradesThisHour   int
	HourStart        time.Time
	AccountBalance   float64
	CanTrade         bool
	WinRate          float64
	TotalTrades      int
	Wins             int
	Losses           int
	CurrentDrawdown  float64
	MinHoldingMet    bool
}

// NewTradingGuardians creates a new safety system
func NewTradingGuardians(initialBalance float64) *TradingGuardians {
	return &TradingGuardians{
		limits: SafetyLimits{
			MaxDailyLoss:      150.0,
			MaxPositionSize:   0.1,
			MinConfidence:     0.85,
			CooldownAfterLoss: 5 * time.Minute,
			MaxTradesPerHour:  120,
			MaxSpread:         0.0005,

			// User-specified rules
			MinConsistency:   0.15, // 15% minimum
			MaxDrawdown:      0.20, // 20% maximum
			MinHoldingTime:   60,   // 60 seconds minimum
		},
		state: &SafetyState{
			AccountBalance: initialBalance,
			CanTrade:       true,
			HourStart:      time.Now(),
			MinHoldingMet:  true,
		},
	}
}

// CanTrade checks if trading is allowed under current conditions
func (g *TradingGuardians) CanTrade(tradeSize float64, aiConfidence float64, spread float64, holdingTime int) bool {
	g.mutex.RLock()
	defer g.mutex.RUnlock()

	return g.checkDailyLoss() &&
		g.checkPositionSize(tradeSize) &&
		g.checkAIConfidence(aiConfidence) &&
		g.checkCooldown() &&
		g.checkTradeFrequency() &&
		g.checkSpread(spread) &&
		g.checkConsistency() &&
		g.checkDrawdown() &&
		g.checkHoldingTime(holdingTime)
}

func (g *TradingGuardians) checkDailyLoss() bool {
	return g.state.DailyLoss < g.limits.MaxDailyLoss
}

func (g *TradingGuardians) checkPositionSize(tradeSize float64) bool {
	maxSize := g.state.AccountBalance * g.limits.MaxPositionSize
	return tradeSize <= maxSize
}

func (g *TradingGuardians) checkAIConfidence(confidence float64) bool {
	return confidence >= g.limits.MinConfidence
}

func (g *TradingGuardians) checkCooldown() bool {
	if g.state.LastLossTime.IsZero() {
		return true
	}
	return time.Since(g.state.LastLossTime) >= g.limits.CooldownAfterLoss
}

func (g *TradingGuardians) checkTradeFrequency() bool {
	// Reset hourly counter if needed
	if time.Since(g.state.HourStart) > time.Hour {
		g.mutex.Lock()
		g.state.TradesThisHour = 0
		g.state.HourStart = time.Now()
		g.mutex.Unlock()
	}

	g.mutex.RLock()
	defer g.mutex.RUnlock()

	return g.state.TradesThisHour < g.limits.MaxTradesPerHour
}

func (g *TradingGuardians) checkSpread(spread float64) bool {
	return spread <= g.limits.MaxSpread
}

// checkConsistency verifies minimum consistency requirement (15%)
func (g *TradingGuardians) checkConsistency() bool {
	if g.state.TotalTrades < 10 {
		return true // Not enough trades to judge
	}
	return g.state.WinRate >= g.limits.MinConsistency
}

// checkDrawdown verifies maximum drawdown limit
func (g *TradingGuardians) checkDrawdown() bool {
	return g.state.CurrentDrawdown <= g.limits.MaxDrawdown
}

// checkHoldingTime verifies minimum holding time requirement
func (g *TradingGuardians) checkHoldingTime(holdingTime int) bool {
	return holdingTime >= g.limits.MinHoldingTime || g.limits.MinHoldingTime == 0
}

// RecordLoss records a trading loss and updates state
func (g *TradingGuardians) RecordLoss(lossAmount float64) {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	g.state.DailyLoss += lossAmount
	g.state.LastLossTime = time.Now()
	g.state.TradesToday++
	g.state.TradesThisHour++
	g.state.Losses++
	g.state.TotalTrades++

	// Update drawdown
	g.state.CurrentDrawdown = calculateDrawdown(g.state.AccountBalance, lossAmount)

	// Update win rate
	if g.state.TotalTrades > 0 {
		g.state.WinRate = float64(g.state.Wins) / float64(g.state.TotalTrades)
	}

	// Check if daily loss limit exceeded
	if g.state.DailyLoss >= g.limits.MaxDailyLoss {
		g.state.CanTrade = false
	}
}

// RecordWin records a winning trade
func (g *TradingGuardians) RecordWin() {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	g.state.TradesToday++
	g.state.TradesThisHour++
	g.state.Wins++
	g.state.TotalTrades++

	// Update win rate
	if g.state.TotalTrades > 0 {
		g.state.WinRate = float64(g.state.Wins) / float64(g.state.TotalTrades)
	}
}

// RecordLossAndWin records a trade result (helper for testing)
func (g *TradingGuardians) RecordTrade(won bool, lossAmount float64) {
	if won {
		g.RecordWin()
	} else {
		g.RecordLoss(lossAmount)
	}
}

// UpdateWinRate updates the win rate after a trade
func (g *TradingGuardians) UpdateWinRate(won bool) {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	g.state.TotalTrades++
	if won {
		g.state.Wins++
	}

	if g.state.TotalTrades > 0 {
		g.state.WinRate = float64(g.state.Wins) / float64(g.state.TotalTrades)
	}
}

// SetDrawdown sets the current drawdown level
func (g *TradingGuardians) SetDrawdown(drawdown float64) {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	g.state.CurrentDrawdown = drawdown
}

// GetStatus returns current safety status
func (g *TradingGuardians) GetStatus() map[string]interface{} {
	g.mutex.RLock()
	defer g.mutex.RUnlock()

	return map[string]interface{}{
		"daily_loss":         g.state.DailyLoss,
		"max_daily_loss":     g.limits.MaxDailyLoss,
		"can_trade":          g.state.CanTrade,
		"trades_today":       g.state.TradesToday,
		"trades_this_hour":   g.state.TradesThisHour,
		"last_loss_time":     g.state.LastLossTime,
		"account_balance":    g.state.AccountBalance,
		"win_rate":           g.state.WinRate,
		"total_trades":       g.state.TotalTrades,
		"current_drawdown":   g.state.CurrentDrawdown,
		"max_drawdown":       g.limits.MaxDrawdown,
		"min_consistency":    g.limits.MinConsistency,
		"min_holding_time":   g.limits.MinHoldingTime,
	}
}

// GetLimits returns safety limits configuration
func (g *TradingGuardians) GetLimits() map[string]interface{} {
	return map[string]interface{}{
		"max_daily_loss":      g.limits.MaxDailyLoss,
		"max_position_pct":    g.limits.MaxPositionSize,
		"min_confidence":      g.limits.MinConfidence,
		"cooldown_after_loss": g.limits.CooldownAfterLoss.String(),
		"max_trades_per_hour": g.limits.MaxTradesPerHour,
		"max_spread":          g.limits.MaxSpread,
		"min_consistency":     g.limits.MinConsistency,
		"max_drawdown":        g.limits.MaxDrawdown,
		"min_holding_time":    g.limits.MinHoldingTime,
	}
}

// EmergencyStop immediately stops all trading
func (g *TradingGuardians) EmergencyStop() {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	g.state.CanTrade = false
}

// ResumeTrading resumes trading after emergency stop
func (g *TradingGuardians) ResumeTrading() {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	g.state.CanTrade = true
}

func calculateDrawdown(currentBalance, loss float64) float64 {
	if currentBalance <= 0 {
		return 0
	}
	return (loss / currentBalance) * 100
}