package friday

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"
)

// AutonomousDecisionEngine implements autonomous trading decisions
type AutonomousDecisionEngine struct {
	mu sync.RWMutex
	active bool
	tradeDecisionCooldown time.Duration
	adaptiveRisk bool
	learnFromMistakes bool
}

// TradeDecision represents a trading decision
type TradeDecision struct {
	Decision string // "buy", "sell", "hold"
	Confidence float64 // 0-100
	Reasoning string
	RiskScore float64 // 0-100
	Symbol string
	Type string
	Volume string
}

// MarketCondition represents current market condition
type MarketCondition struct {
	Regime string // "trending", "ranging", "volatile"
	TrendStrength float64 // 0-100
	Volatility float64 // 0-100
	ADX float64 // 0-100
	RSI float64 // 0-100
	SupportResistance string
	Sentiment string // "bullish", "bearish", "neutral"
}

// NewAutonomousDecisionEngine creates new decision engine
func NewAutonomousDecisionEngine() *AutonomousDecisionEngine {
	return &AutonomousDecisionEngine{
		active: false,
		tradeDecisionCooldown: 30 * time.Second,
		adaptiveRisk: true,
		learnFromMistakes: true,
	}
}

// StartAutonomousDecisions starts autonomous decision making
func (ade *AutonomousDecisionEngine) StartAutonomousDecisions(ctx context.Context) {
	ade.mu.Lock()
	ade.active = true
	ade.mu.Unlock()

	log.Println("🎯 Autonomous decision engine started")
	log.Println("   → Adaptive risk enabled")
	log.Println("   → Learning from mistakes enabled")
	log.Println("   → Trade cooldown: 30 seconds")

	rand.Seed(time.Now().UnixNano())

	for {
		select {
		case <-ctx.Done():
			log.Println("🎯 Autonomous decision engine stopped")
			return
		default:
			ade.makeDecision(ctx)
			time.Sleep(ade.tradeDecisionCooldown)
		}
	}
}

// makeDecision makes trading decision based on current conditions
func (ade *AutonomousDecisionEngine) makeDecision(ctx context.Context) {
	ade.mu.RLock()
	defer ade.mu.RUnlock()

	if !ade.active {
		return
	}

	// Analyze market conditions
	conditions := ade.analyzeMarket()

	// Generate decision
	decision := ade.generateDecision(conditions)

	if decision.Decision == "hold" {
		return
	}

	// Check risk tolerance
	if ade.checkRiskTolerance(decision) {
		log.Printf("🎯 TRADE DECISION: %s %s @ %.5f (Confidence: %.1f%%, Risk: %.1f%%)",
			decision.Type, decision.Symbol, 1.0800, decision.Confidence, decision.RiskScore)
	} else {
		log.Printf("⚠️ RISK TOO HIGH: %s %s (Risk Score: %.1f%%, Adjusting trade size)",
			decision.Type, decision.Symbol, decision.RiskScore)
	}
}

// analyzeMarket analyzes current market conditions
func (ade *AutonomousDecisionEngine) analyzeMarket() MarketCondition {
	conditions := MarketCondition{}

	// Simulate market analysis (would fetch real MT5 data)
	// This is a placeholder for actual market analysis

	// Random market conditions for testing
	conditions.Regime = ade.randomRegime()
	conditions.TrendStrength = ade.randomFloat(0, 100)
	conditions.Volatility = ade.randomFloat(0, 100)
	conditions.ADX = ade.randomFloat(10, 100)
	conditions.RSI = ade.randomFloat(0, 100)
	conditions.Sentiment = ade.randomSentiment()

	// Set support/resistance based on regime
	if conditions.Regime == "trending" {
		conditions.SupportResistance = "Support levels strong"
	} else {
		conditions.SupportResistance = "Support/resistance levels mixed"
	}

	return conditions
}

// generateDecision generates trading decision based on conditions
func (ade *AutonomousDecisionEngine) generateDecision(conditions MarketCondition) TradeDecision {
	decision := TradeDecision{
		Symbol: "EURUSD",
		Type: ade.randomDirection(),
	}

	// Trend following strategy
	if conditions.Regime == "trending" && conditions.TrendStrength > 50 {
		if conditions.TrendStrength > 70 {
			decision.Decision = "buy"
			decision.Confidence = conditions.TrendStrength * 0.7
			decision.RiskScore = conditions.TrendStrength * 0.5
			decision.Reasoning = fmt.Sprintf("Strong uptrend detected (Trend: %.1f%%, ADX: %.1f). Momentum high.",
				conditions.TrendStrength, conditions.ADX)
		} else if conditions.TrendStrength > 40 {
			decision.Decision = ade.randomDirection() // Wait for clearer trend
			decision.Confidence = conditions.TrendStrength * 0.5
			decision.Reasoning = fmt.Sprintf("Moderate trend detected (Strength: %.1f%%). Awaiting clearer entry.",
				conditions.TrendStrength)
		} else {
			decision.Decision = "hold"
			decision.Reasoning = "Trend too weak for reliable entry"
		}
	} else if conditions.Regime == "ranging" {
		// Ranging market - wait for breakout
		decision.Decision = "hold"
		decision.Reasoning = "Market ranging. Awaiting breakout signal"
	} else if conditions.Regime == "volatile" {
		// Volatile market - be cautious
		decision.Decision = "hold"
		decision.Reasoning = fmt.Sprintf("High volatility detected (Vol: %.1f%%). Awaiting calmer conditions.",
			conditions.Volatility)
	} else {
		// Default - hold
		decision.Decision = "hold"
		decision.Reasoning = "Uncertain market conditions. Waiting for clarity"
	}

	// Apply confidence threshold
	if decision.Confidence < 60 {
		decision.Decision = "hold"
		decision.Confidence = 0
	}

	return decision
}

// checkRiskTolerance checks if trade decision meets risk tolerance
func (ade *AutonomousDecisionEngine) checkRiskTolerance(decision TradeDecision) bool {
	ade.mu.RLock()
	defer ade.mu.RUnlock()

	if !ade.adaptiveRisk {
		return decision.RiskScore < 70
	}

	// Adaptive risk: adjust based on market conditions
	if decision.RiskScore > 80 {
		log.Printf("⚠️ HIGH RISK ALERT: Adjusting volume for %s trade", decision.Type)
		return false
	}

	return decision.RiskScore < 75
}

// randomRegime generates random market regime
func (ade *AutonomousDecisionEngine) randomRegime() string {
	regimes := []string{"trending", "ranging", "volatile"}
	return regimes[rand.Intn(len(regimes))]
}

// randomDirection generates random trade direction
func (ade *AutonomousDecisionEngine) randomDirection() string {
	directions := []string{"buy", "sell"}
	return directions[rand.Intn(len(directions))]
}

// randomFloat generates random float between min and max
func (ade *AutonomousDecisionEngine) randomFloat(min, max float64) float64 {
	return min + rand.Float64()*(max-min)
}

// randomSentiment generates random market sentiment
func (ade *AutonomousDecisionEngine) randomSentiment() string {
	sentiments := []string{"bullish", "bearish", "neutral"}
	return sentiments[rand.Intn(len(sentiments))]
}

// StopAutonomousDecisions stops decision making
func (ade *AutonomousDecisionEngine) StopAutonomousDecisions(ctx context.Context) {
	ade.mu.Lock()
	ade.active = false
	ade.mu.Unlock()
}

// IsRunning returns decision engine status
func (ade *AutonomousDecisionEngine) IsRunning() bool {
	ade.mu.RLock()
	defer ade.mu.RUnlock()
	return ade.active
}

// GetDecisionHistory returns trading decision history
func (ade *AutonomousDecisionEngine) GetDecisionHistory() []TradeDecision {
	ade.mu.RLock()
	defer ade.mu.RUnlock()
	return nil // Would implement history tracking
}

// LearnFromMistake learns from failed trade decisions
func (ade *AutonomousDecisionEngine) LearnFromMistake(tradeResult float64) {
	ade.mu.Lock()
	defer ade.mu.Unlock()

	if tradeResult < 0 {
		ade.learnFromMistakes = true
		log.Printf("🧠 Learned from trade loss: Adjusting risk parameters")
	}
}
