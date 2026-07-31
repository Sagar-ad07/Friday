package friday

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// PropFirmConfig holds the rules for a prop firm challenge
type PropFirmConfig struct {
	Name           string  `json:"name"`
	AccountSize    float64 `json:"account_size"`
	MaxDailyLoss   float64 `json:"max_daily_loss"`
	MaxDrawdown    float64 `json:"max_drawdown"`    // % of account
	ProfitTarget   float64 `json:"profit_target"`
	ConsistencyPct float64 `json:"consistency_pct"` // best day ≤ this % of total profit
	MinTradingDays int     `json:"min_trading_days"`
}

// PropFirmState tracks live compliance state
type PropFirmState struct {
	mu sync.RWMutex

	Config     PropFirmConfig      `json:"config"`
	DailyPnL   float64             `json:"daily_pnl"`
	TotalPnL   float64             `json:"total_pnl"`
	PeakBalance float64            `json:"peak_balance"`
	DayHistory []float64           `json:"day_history"`
	LastReset  time.Time           `json:"last_reset"`
	TradesToday int                `json:"trades_today"`
	Violations []string            `json:"violations"`
	TradingActive bool             `json:"trading_active"`
	LastError  string              `json:"last_error,omitempty"`
}

var propFirm *PropFirmState
var propFirmOnce sync.Once

func GetPropFirm() *PropFirmState {
	propFirmOnce.Do(func() {
		path := filepath.Join(ProjectRoot, "data", "propfirm.json")
		pf := &PropFirmState{
			Config: PropFirmConfig{
				Name:           "Blue Guardian $5k Instant Starter",
				AccountSize:    5000,
				MaxDailyLoss:   150,
				MaxDrawdown:    5.0,
				ProfitTarget:   250,
				ConsistencyPct: 15.0,
				MinTradingDays: 5,
			},
			TradingActive: true,
			LastReset:     time.Now(),
		}
		if data, err := os.ReadFile(path); err == nil {
			if json.Unmarshal(data, pf) == nil {
				log.Printf("PropFirm loaded: balance=$%.0f, daily=$%.2f, total=$%.2f, trades=%d",
					pf.Config.AccountSize, pf.DailyPnL, pf.TotalPnL, pf.TradesToday)
			}
		}
		propFirm = pf
	})
	return propFirm
}

func (pf *PropFirmState) save() {
	p := filepath.Join(ProjectRoot, "data", "propfirm.json")
	os.MkdirAll(filepath.Dir(p), 0755)
	data, _ := json.MarshalIndent(pf, "", "  ")
	os.WriteFile(p, data, 0644)
}

// RecordTrade logs a trade and checks compliance
// Returns (allowed, violationMessage)
func (pf *PropFirmState) RecordTrade(pnl float64) (bool, string) {
	pf.mu.Lock()
	defer pf.mu.Unlock()

	// Daily reset check
	now := time.Now()
	if now.Day() != pf.LastReset.Day() || now.Month() != pf.LastReset.Month() || now.Year() != pf.LastReset.Year() {
		if pf.DailyPnL != 0 {
			pf.DayHistory = append(pf.DayHistory, pf.DailyPnL)
		}
		pf.DailyPnL = 0
		pf.TradesToday = 0
		pf.LastReset = now
	}

	pf.DailyPnL += pnl
	pf.TotalPnL += pnl
	pf.TradesToday++

	currentBalance := pf.Config.AccountSize + pf.TotalPnL
	if currentBalance > pf.PeakBalance {
		pf.PeakBalance = currentBalance
	}

	// Check violations
	violation := pf.checkRules(currentBalance)
	if violation != "" {
		pf.Violations = append(pf.Violations, fmt.Sprintf("%s: %s", now.Format("2006-01-02 15:04"), violation))
		if len(pf.Violations) > 100 {
			pf.Violations = pf.Violations[len(pf.Violations)-100:]
		}
		pf.TradingActive = false
		pf.LastError = violation
		pf.save()
		return false, violation
	}

	// Check if profit target reached
	if pf.TotalPnL >= pf.Config.ProfitTarget {
		pf.TradingActive = false
		pf.LastError = fmt.Sprintf("Profit target $%.0f reached! Total PnL: $%.2f", pf.Config.ProfitTarget, pf.TotalPnL)
		pf.save()
		return false, pf.LastError
	}

	pf.save()
	return true, ""
}

func (pf *PropFirmState) checkRules(balance float64) string {
	// Max daily loss
	if pf.DailyPnL <= -pf.Config.MaxDailyLoss {
		return fmt.Sprintf("DAILY LOSS LIMIT: $%.2f (max $%.0f)", pf.DailyPnL, -pf.Config.MaxDailyLoss)
	}

	// Max drawdown
	maxBalance := pf.Config.AccountSize
	for _, d := range pf.DayHistory {
		bal := pf.Config.AccountSize + d
		if bal > maxBalance { maxBalance = bal }
	}
	if pf.TotalPnL > 0 {
		peakBal := pf.Config.AccountSize + pf.TotalPnL
		if peakBal > maxBalance { maxBalance = peakBal }
	}
	drawdownPct := (maxBalance - balance) / maxBalance * 100
	if drawdownPct > pf.Config.MaxDrawdown {
		return fmt.Sprintf("DRAWDOWN LIMIT: %.1f%% (max %.0f%%)", drawdownPct, pf.Config.MaxDrawdown)
	}

	// Consistency rule (if enough data)
	totalProfit := 0.0
	for _, d := range pf.DayHistory {
		if d > 0 { totalProfit += d }
	}
	if pf.TotalPnL > 0 { totalProfit += pf.DailyPnL }
	if totalProfit > 0 && len(pf.DayHistory) > 0 {
		maxDay := 0.0
		for _, d := range pf.DayHistory {
			if d > maxDay { maxDay = d }
		}
		if pf.DailyPnL > maxDay { maxDay = pf.DailyPnL }
		if maxDay/totalProfit*100 > pf.Config.ConsistencyPct {
			return fmt.Sprintf("CONSISTENCY RULE: Best day $%.2f is %.1f%% of total $%.2f (max %.0f%%)",
				maxDay, maxDay/totalProfit*100, totalProfit, pf.Config.ConsistencyPct)
		}
	}

	return ""
}

// MaxLotSize returns the maximum lot size that won't exceed daily loss limit
func (pf *PropFirmState) MaxLotSize(symbol string, stopLossPips float64) float64 {
	pf.mu.RLock()
	defer pf.mu.RUnlock()

	remainingDaily := pf.Config.MaxDailyLoss + pf.DailyPnL // how much more we can lose today
	if remainingDaily <= 0 { return 0 }

	// Conservative: risk 20% of remaining daily budget per trade
	riskBudget := remainingDaily * 0.2
	pipValue := 1.0 // $1 per pip for standard lot on EURUSD
	if stopLossPips <= 0 { stopLossPips = 20 }

	lot := riskBudget / (stopLossPips * pipValue)
	if lot < 0.01 { lot = 0.01 }
	if lot > 0.05 { lot = 0.05 } // never exceed 0.05 lots on $5k
	return math.Floor(lot*100) / 100
}

// Status returns a human-readable compliance report
func (pf *PropFirmState) Status() string {
	pf.mu.RLock()
	defer pf.mu.RUnlock()

	s := fmt.Sprintf("=== Blue Guardian $5k Instant Starter ===\n")
	s += fmt.Sprintf("Trading: %v\n", pf.TradingActive)
	s += fmt.Sprintf("Balance: $%.2f\n", pf.Config.AccountSize+pf.TotalPnL)
	s += fmt.Sprintf("Total P&L: $%.2f\n", pf.TotalPnL)
	s += fmt.Sprintf("Today: $%.2f (%d trades)\n", pf.DailyPnL, pf.TradesToday)
	s += fmt.Sprintf("Peak Balance: $%.2f\n", pf.PeakBalance)
	if pf.TotalPnL > 0 && pf.PeakBalance > 0 {
		dd := (pf.PeakBalance - (pf.Config.AccountSize + pf.TotalPnL)) / pf.PeakBalance * 100
		s += fmt.Sprintf("Current Drawdown: %.1f%%\n", dd)
	}
	s += "\n"
	s += fmt.Sprintf("Rules:\n")
	s += fmt.Sprintf("  Daily Loss Limit: $%.0f (remaining: $%.0f)\n", pf.Config.MaxDailyLoss, pf.Config.MaxDailyLoss+pf.DailyPnL)
	s += fmt.Sprintf("  Max Drawdown: %.0f%%\n", pf.Config.MaxDrawdown)
	s += fmt.Sprintf("  Profit Target: $%.0f (progress: %.1f%%)\n", pf.Config.ProfitTarget, pf.TotalPnL/pf.Config.ProfitTarget*100)
	s += fmt.Sprintf("  Consistency: Best day ≤ %.0f%% of total\n", pf.Config.ConsistencyPct)
	if len(pf.Violations) > 0 {
		s += "\nViolations:\n"
		for _, v := range pf.Violations {
			s += fmt.Sprintf("  - %s\n", v)
		}
	}
	return s
}
