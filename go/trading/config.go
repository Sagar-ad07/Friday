package trading

import (
	"fmt"
	"log"
	"os"
	"sync"
)

// TradingConfig holds all trading configuration
type TradingConfig struct {
	mu sync.RWMutex

	// Account settings
	Symbol         string
	Timeframe      string
	MT5Login       int
	MT5Password    string
	MT5Server      string
	AccountBalance float64

	// Risk profiles
	RiskProfiles map[string]RiskProfile
	ActiveProfile string

	// Risk limits
	MaxProfit           float64
	OverallDrawdownPct  float64
	MinHoldSec          int
	MinTradingDays      int
	DailyLossLimit      float64
	GuardianShieldPct   float64
	MinDailyProfitPct   float64

	// Strategy parameters
	RangeBuildMinutes   int
	TradeWindowMinutes  int
	MaxHoldMinutes      int
	MinRangeFilterPips  int

	// Consistency and drawdown rules (user specified)
	MinConsistency      float64 // 15% minimum consistency
	MaxDrawdown         float64 // Maximum drawdown limit
	MinHoldingTime      int     // Minimum time to hold a trade

	// Mode settings
	LiveMode    bool
	RookieMode  bool

	// Correlation settings
	CorrelationBlock bool

	// Performance tracking
	Stats TradingStats
}

// RiskProfile defines risk parameters for a profile
type RiskProfile struct {
	Risk        float64
	Reward      float64
	SL          int
	TP          int
	MinBalance  float64
}

// TradingStats tracks performance statistics
type TradingStats struct {
	mu sync.Mutex

	TotalTrades   int
	Wins          int
	Losses        int
	TotalPNL      float64
	DailyPNL      float64
	MaxDrawdown   float64
	Consistency   float64 // Win rate percentage
}

var tradingConfig *TradingConfig
var once sync.Once

// InitTradingConfig initializes the trading configuration
func InitTradingConfig() *TradingConfig {
	once.Do(func() {
		tradingConfig = &TradingConfig{
			Symbol:           "EURUSDm",
			Timeframe:          "H1", // H1 for swing trading (M1 = scalping, prohibited by BG)
			// Primary: Exness — Secondary: Blue Guardian
			MT5Login:           envInt("MT5_LOGIN", envInt("EXNESS_LOGIN", 503985)),
			MT5Password:        envStr("MT5_PASSWORD", envStr("EXNESS_PASSWORD", "mSz$1Kyr1(")),
			MT5Server:          envStr("MT5_SERVER", envStr("EXNESS_SERVER", "BlueGuardian-Server")),
			AccountBalance:     5000.0, // Blue Guardian $5k Instant Starter

			// Risk profiles — Blue Guardian Instant Starter compliance
			RiskProfiles: map[string]RiskProfile{
				"micro":        {Risk: 4.0, Reward: 8.0, SL: 8, TP: 16, MinBalance: 8},
				"small":        {Risk: 8.0, Reward: 16.0, SL: 8, TP: 16, MinBalance: 16},
				"medium":       {Risk: 12.0, Reward: 24.0, SL: 8, TP: 16, MinBalance: 32},
				"standard":     {Risk: 18.0, Reward: 36.0, SL: 8, TP: 16, MinBalance: 56},
				"blueguardian": {Risk: 25.0, Reward: 50.0, SL: 8, TP: 16, MinBalance: 5000},
			},

			// Risk limits — Blue Guardian Instant Starter rules
			MaxProfit:          250.0,  // $250 profit target
			OverallDrawdownPct: 0.05,  // 5% trailing drawdown
			MinHoldSec:         30,
			MinTradingDays:     7,     // Minimum 7 trading days
			DailyLossLimit:     150.0, // $150 daily loss limit
			GuardianShieldPct:  0.50,  // 50% of risk amount
			MinDailyProfitPct:  0.05,

			// Strategy parameters
			RangeBuildMinutes:  30,
			TradeWindowMinutes: 90,
			MaxHoldMinutes:     360,
			MinRangeFilterPips: 4,

			// Blue Guardian consistency and drawdown rules
			MinConsistency:     0.15, // 15% best day cap
			MaxDrawdown:        0.05, // 5% trailing drawdown
			MinHoldingTime:     60,   // 60 seconds minimum holding time

			// Mode settings
			LiveMode:   true,
			RookieMode: true,

			// Correlation settings
			CorrelationBlock: true,
		}

		// Auto-select profile based on balance
		tradingConfig.ActiveProfile = tradingConfig.autoSelectProfile()

		// Log initialization
		log.Printf("Trading config initialized: Profile=%s, Balance=$%.2f",
			tradingConfig.ActiveProfile, tradingConfig.AccountBalance)
	})
	return tradingConfig
}

// Auto-select profile based on account balance
func (c *TradingConfig) autoSelectProfile() string {
	bal := c.AccountBalance
	best := "micro"
	for name, profile := range c.RiskProfiles {
		if bal >= profile.MinBalance {
			best = name
		}
	}
	return best
}

// GetRiskParams returns current risk parameters
func (c *TradingConfig) GetRiskParams() RiskProfile {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.RiskProfiles[c.ActiveProfile]
}

// RiskAmount returns the current risk amount in USD
func (c *TradingConfig) RiskAmount() float64 {
	return c.GetRiskParams().Risk
}

// RewardAmount returns the current reward amount in USD
func (c *TradingConfig) RewardAmount() float64 {
	return c.GetRiskParams().Reward
}

// StopLossPips returns the stop loss in pips
func (c *TradingConfig) StopLossPips() int {
	return c.GetRiskParams().SL
}

// TakeProfitPips returns the take profit in pips
func (c *TradingConfig) TakeProfitPips() int {
	return c.GetRiskParams().TP
}

// KellyFraction calculates optimal position size using Kelly criterion
func KellyFraction(winRate, riskRewardRatio float64) float64 {
	if winRate <= 0 || riskRewardRatio <= 0 {
		return 0.01
	}
	p := winRate / 100.0
	q := 1 - p
	b := riskRewardRatio
	kelly := p - (q / b)
	return min(0.05, max(0.01, kelly*0.25))
}

// CheckConsistency checks if trading meets minimum consistency requirement
func (s *TradingStats) CheckConsistency() bool {
	if s.TotalTrades < 10 {
		return true // Not enough trades to judge
	}
	return float64(s.Wins)/float64(s.TotalTrades) >= 0.50 // 50% win rate minimum
}

// CheckDrawdown checks if drawdown is within limits
func (s *TradingStats) CheckDrawdown(balance float64) bool {
	if balance <= 0 {
		return false
	}
	currentDrawdown := (s.MaxDrawdown - balance) / s.MaxDrawdown
	return currentDrawdown <= 0.20 // 20% max drawdown
}

// UpdateStats updates trading statistics
func (s *TradingStats) UpdateTrade(won bool, pnl float64, balance float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TotalTrades++
	if won {
		s.Wins++
	} else {
		s.Losses++
	}
	s.TotalPNL += pnl
	s.DailyPNL += pnl

	// Update consistency
	if s.TotalTrades > 0 {
		s.Consistency = float64(s.Wins) / float64(s.TotalTrades)
	}

	// Update max drawdown
	if balance > 0 && pnl < 0 {
		// Track running drawdown
	}
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return fallback
}

// LoadTradingConfigFromEnv loads configuration from environment variables
func LoadTradingConfigFromEnv() {
	if tradingConfig == nil {
		return
	}

	tradingConfig.mu.Lock()
	defer tradingConfig.mu.Unlock()

	if v := os.Getenv("SYMBOL"); v != "" {
		tradingConfig.Symbol = v
	}
	if v := os.Getenv("MT5_LOGIN"); v != "" {
		if n, err := fmt.Sscanf(v, "%d", &tradingConfig.MT5Login); err != nil || n != 1 {
			log.Printf("Invalid MT5_LOGIN: %s", v)
		}
	}
	if v := os.Getenv("MT5_PASSWORD"); v != "" {
		tradingConfig.MT5Password = v
	}
	if v := os.Getenv("MT5_SERVER"); v != "" {
		tradingConfig.MT5Server = v
	}
	if v := os.Getenv("MIN_CONSISTENCY"); v != "" {
		if f, err := fmt.Sscanf(v, "%f", &tradingConfig.MinConsistency); err != nil || f != 1 {
			log.Printf("Invalid MIN_CONSISTENCY: %s", v)
		}
	}
	if v := os.Getenv("MAX_DRAWDOWN"); v != "" {
		if f, err := fmt.Sscanf(v, "%f", &tradingConfig.MaxDrawdown); err != nil || f != 1 {
			log.Printf("Invalid MAX_DRAWDOWN: %s", v)
		}
	}
	if v := os.Getenv("MIN_HOLDING_TIME"); v != "" {
		if n, err := fmt.Sscanf(v, "%d", &tradingConfig.MinHoldingTime); err != nil || n != 1 {
			log.Printf("Invalid MIN_HOLDING_TIME: %s", v)
		}
	}
	if v := os.Getenv("LIVE_MODE"); v == "true" {
		tradingConfig.LiveMode = true
	}
	if v := os.Getenv("ROOKIE_MODE"); v == "true" {
		tradingConfig.RookieMode = true
	}
	log.Printf("Trading config reloaded from env: Symbol=%s, Login=%d, Server=%s",
		tradingConfig.Symbol, tradingConfig.MT5Login, tradingConfig.MT5Server)
}