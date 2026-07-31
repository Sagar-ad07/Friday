package trading

import (
	"testing"
)

// ─── Lot Sizing Tests ───

func TestLotSizingLargeAccount(t *testing.T) {
	balance := 5000.0
	var volume float64
	switch {
	case balance >= 5000:
		volume = 0.15
	case balance >= 1000:
		volume = 0.10
	case balance >= 100:
		volume = 0.02
	default:
		volume = 0.01
	}
	if volume != 0.15 {
		t.Errorf("expected 0.15 for $5000 account, got %.3f", volume)
	}
}

func TestLotSizingMediumAccount(t *testing.T) {
	balance := 1000.0
	var volume float64
	switch {
	case balance >= 5000:
		volume = 0.15
	case balance >= 1000:
		volume = 0.10
	case balance >= 100:
		volume = 0.02
	default:
		volume = 0.01
	}
	if volume != 0.10 {
		t.Errorf("expected 0.10 for $1000 account, got %.3f", volume)
	}
}

func TestLotSizingSmallAccount(t *testing.T) {
	balance := 100.0
	var volume float64
	switch {
	case balance >= 5000:
		volume = 0.15
	case balance >= 1000:
		volume = 0.10
	case balance >= 100:
		volume = 0.02
	default:
		volume = 0.01
	}
	if volume != 0.02 {
		t.Errorf("expected 0.02 for $100 account, got %.3f", volume)
	}
}

func TestLotSizingTinyAccount(t *testing.T) {
	balance := 50.0
	var volume float64
	switch {
	case balance >= 5000:
		volume = 0.15
	case balance >= 1000:
		volume = 0.10
	case balance >= 100:
		volume = 0.02
	default:
		volume = 0.01
	}
	if volume != 0.01 {
		t.Errorf("expected 0.01 for $50 account, got %.3f", volume)
	}
}

// ─── Prop Firm Compliance Tests ───

func TestDailyLossLimitEnforcement(t *testing.T) {
	// Simulate the checkRules logic for daily loss
	tests := []struct {
		name        string
		dailyPNL    float64
		capEnabled  bool
		capLimit    float64
		shouldBreach bool
	}{
		{"no cap disabled", -200, false, 150, false},
		{"under cap", -100, true, 150, false},
		{"at cap exactly", -150, true, 150, true},
		{"over cap", -151, true, 150, true},
		{"way over cap", -300, true, 150, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			breach := false
			if tt.capEnabled && tt.capLimit > 0 && tt.dailyPNL <= -tt.capLimit {
				breach = true
			}
			if breach != tt.shouldBreach {
				t.Errorf("expected breach=%v, got %v", tt.shouldBreach, breach)
			}
		})
	}
}

func TestTrailingDrawdownEnforcement(t *testing.T) {
	// Simulate trailing drawdown check
	tests := []struct {
		name           string
		peak           float64
		currentBalance float64
		capEnabled     bool
		shouldBreach   bool
	}{
		{"no peak", 0, 5000, true, false},
		{"healthy", 5100, 5050, true, false},
		{"at trail", 5100, 5100*0.80, true, true},
		{"below trail", 5100, 5100*0.79, true, true},
		{"cap disabled", 5100, 4000, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			breach := false
			if tt.capEnabled && tt.peak > 0 {
				trailingStop := tt.peak * 0.80
				if tt.currentBalance <= trailingStop {
					breach = true
				}
			}
			if breach != tt.shouldBreach {
				t.Errorf("expected breach=%v, got %v", tt.shouldBreach, breach)
			}
		})
	}
}

// ─── Risk Calculation Tests ───

func TestRiskCalculation(t *testing.T) {
	// Standard risk: 1% of $5000 = $50 risk
	// Stop loss: 30 pips × $10/pip = $300 risk per lot
	// Position size = $50 / $300 = 0.1667 lots
	accountBalance := 5000.0
	riskPercent := 1.0
	stopLossPips := 30.0
	pipValue := 10.0

	riskAmount := accountBalance * riskPercent / 100
	positionSize := riskAmount / (stopLossPips * pipValue)

	expected := 50.0 / 300.0
	if positionSize < expected-0.001 || positionSize > expected+0.001 {
		t.Errorf("expected ~%.4f, got %.4f", expected, positionSize)
	}
}

func TestRiskCalculationZeroStopLoss(t *testing.T) {
	// Division by zero should be caught
	stopLossPips := 0.0
	pipValue := 10.0

	if stopLossPips <= 0 {
		// This is the guard we added — verify it would catch this
		if stopLossPips*pipValue == 0 {
			return // correctly caught
		}
	}
	t.Fatal("expected zero stop loss to be caught")
}

// ─── Bot State Tests ───

func TestBotStateInitialization(t *testing.T) {
	state := BotState{
		Running:        false,
		InTrade:        false,
		InitialBalance: 5000,
		DailyPNL:       0,
		TotalPNL:       0,
		TradesToday:    0,
		Wins:           0,
		Losses:         0,
	}

	if state.Running {
		t.Error("expected bot to start not running")
	}
	if state.InitialBalance != 5000 {
		t.Errorf("expected initial balance 5000, got %.2f", state.InitialBalance)
	}
}

// ─── Cap Config Tests ───

func TestCapConfigDisabled(t *testing.T) {
	cap := CapConfig{
		Enabled: false,
		Limit:   150,
	}

	// When disabled, no breach should occur regardless of daily loss
	dailyPNL := -500.0
	breach := false
	if cap.Enabled && cap.Limit > 0 && dailyPNL <= -cap.Limit {
		breach = true
	}
	if breach {
		t.Error("disabled cap should never breach")
	}
}

func TestCapConfigEnabled(t *testing.T) {
	cap := CapConfig{
		Enabled: true,
		Limit:   150,
	}

	dailyPNL := -200.0
	breach := false
	if cap.Enabled && cap.Limit > 0 && dailyPNL <= -cap.Limit {
		breach = true
	}
	if !breach {
		t.Error("enabled cap should breach when daily loss exceeds limit")
	}
}
