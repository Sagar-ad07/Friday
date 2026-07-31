package trading

import (
	"time"
)

// Account represents a trading account
type Account struct {
	ID             string
	Login          int
	Server         string
	Currency       string
	Leverage       int
	Balance        float64
	Equity         float64
	Margin         float64
	FreeMargin     float64
	MarginLevel    float64
	Profit         float64
	DailyPnL       float64
	TotalPnL       float64
	OpenPositions  int
	OpenOrders     int
	MarginCallLevel float64
	StopOutLevel   float64
	UpdatedAt      time.Time
}

func NewAccount(login int, server, currency string, leverage int, balance float64) *Account {
	return &Account{
		ID:              generateAccountID(login),
		Login:           login,
		Server:          server,
		Currency:        currency,
		Leverage:        leverage,
		Balance:         balance,
		Equity:          balance,
		FreeMargin:      balance,
		MarginLevel:     0,
		MarginCallLevel: 50,  // 50%
		StopOutLevel:    20,  // 20%
		UpdatedAt:       time.Now().UTC(),
	}
}

func (a *Account) UpdateEquity(equity, margin, profit float64) {
	a.Equity = equity
	a.Margin = margin
	a.Profit = profit
	a.FreeMargin = equity - margin
	if margin > 0 {
		a.MarginLevel = (equity / margin) * 100
	}
	a.UpdatedAt = time.Now().UTC()
}

func (a *Account) UpdateBalance(balance float64) {
	a.Balance = balance
	a.Equity = balance + a.Profit
	a.FreeMargin = a.Equity - a.Margin
	if a.Margin > 0 {
		a.MarginLevel = (a.Equity / a.Margin) * 100
	}
	a.UpdatedAt = time.Now().UTC()
}

func (a *Account) AddDailyPnL(pnl float64) {
	a.DailyPnL += pnl
	a.TotalPnL += pnl
}

func (a *Account) ResetDailyPnL() {
	a.DailyPnL = 0
}

func (a *Account) IsMarginCall() bool {
	return a.MarginLevel <= float64(a.MarginCallLevel)
}

func (a *Account) IsStopOut() bool {
	return a.MarginLevel <= float64(a.StopOutLevel)
}

func (a *Account) CanOpenPosition(requiredMargin float64) bool {
	return a.FreeMargin >= requiredMargin && !a.IsMarginCall() && !a.IsStopOut()
}

func (a *Account) RiskAmount(riskPercent float64) float64 {
	return a.Equity * (riskPercent / 100.0)
}

func generateAccountID(login int) string {
	return "acc_" + string(rune(login)) + "_" + time.Now().UTC().Format("20060102")
}