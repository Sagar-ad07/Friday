package trading

import (
	"time"

	"github.com/friday-prototype/friday-go/pkg/util"
)

// Position represents an open trading position
type Position struct {
	ID            string
	Symbol        string
	Side          Side
	Size          float64
	EntryPrice    float64
	CurrentPrice  float64
	StopLoss      float64
	TakeProfit    float64
	UnrealizedPnL float64
	RealizedPnL   float64
	TotalPnL      float64
	Commission    float64
	Swap          float64
	MarginUsed    float64
	Leverage      int
	OpenedAt      time.Time
	UpdatedAt     time.Time
	ClosedAt      *time.Time
	Strategy      string
	OrderIDs      []string
	Metadata      map[string]any
}

func NewPosition(symbol string, side Side, size, entryPrice, stopLoss, takeProfit float64, leverage int) *Position {
	return &Position{
		ID:           util.GenerateIDWithPrefix("pos"),
		Symbol:       symbol,
		Side:         side,
		Size:         size,
		EntryPrice:   entryPrice,
		CurrentPrice: entryPrice,
		StopLoss:     stopLoss,
		TakeProfit:   takeProfit,
		Leverage:     leverage,
		OpenedAt:     time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		OrderIDs:     make([]string, 0),
		Metadata:     make(map[string]any),
	}
}

func (p *Position) UpdatePrice(currentPrice float64) {
	p.CurrentPrice = currentPrice
	p.UnrealizedPnL = p.CalculateUnrealizedPnL()
	p.TotalPnL = p.RealizedPnL + p.UnrealizedPnL
	p.UpdatedAt = time.Now().UTC()
}

func (p *Position) CalculateUnrealizedPnL() float64 {
	diff := p.CurrentPrice - p.EntryPrice
	if p.Side == SideSell {
		diff = -diff
	}
	return diff * p.Size
}

func (p *Position) AddOrderID(orderID string) {
	p.OrderIDs = append(p.OrderIDs, orderID)
}

func (p *Position) IsStopLossHit() bool {
	if p.Side == SideBuy {
		return p.CurrentPrice <= p.StopLoss
	}
	return p.CurrentPrice >= p.StopLoss
}

func (p *Position) IsTakeProfitHit() bool {
	if p.Side == SideBuy {
		return p.CurrentPrice >= p.TakeProfit
	}
	return p.CurrentPrice <= p.TakeProfit
}

func (p *Position) ShouldClose() bool {
	return p.IsStopLossHit() || p.IsTakeProfitHit()
}

func (p *Position) ClosePrice() float64 {
	if p.IsStopLossHit() {
		return p.StopLoss
	}
	if p.IsTakeProfitHit() {
		return p.TakeProfit
	}
	return p.CurrentPrice
}

func (p *Position) Close(closePrice float64, commission float64) {
	p.RealizedPnL = p.CalculateUnrealizedPnLAt(closePrice) - p.Commission - commission
	p.Commission += commission
	p.UnrealizedPnL = 0
	p.TotalPnL = p.RealizedPnL
	p.CurrentPrice = closePrice
	now := time.Now().UTC()
	p.ClosedAt = &now
	p.UpdatedAt = now
}

func (p *Position) CalculateUnrealizedPnLAt(price float64) float64 {
	diff := price - p.EntryPrice
	if p.Side == SideSell {
		diff = -diff
	}
	return diff * p.Size
}

func (p *Position) Duration() time.Duration {
	end := p.UpdatedAt
	if p.ClosedAt != nil {
		end = *p.ClosedAt
	}
	return end.Sub(p.OpenedAt)
}

func (p *Position) ReturnPct() float64 {
	if p.EntryPrice == 0 {
		return 0
	}
	return (p.TotalPnL / (p.EntryPrice * p.Size)) * 100
}

func (p *Position) RiskRewardRatio() float64 {
	if p.Side == SideBuy {
		risk := p.EntryPrice - p.StopLoss
		reward := p.TakeProfit - p.EntryPrice
		if risk <= 0 {
			return 0
		}
		return reward / risk
	}
	risk := p.StopLoss - p.EntryPrice
	reward := p.EntryPrice - p.TakeProfit
	if risk <= 0 {
		return 0
	}
	return reward / risk
}