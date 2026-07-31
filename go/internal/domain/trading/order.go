package trading

import (
	"time"

	"github.com/friday-prototype/friday-go/pkg/errors"
	"github.com/friday-prototype/friday-go/pkg/util"
)

// OrderType represents the type of order
type OrderType string

const (
	OrderTypeMarket    OrderType = "MARKET"
	OrderTypeLimit     OrderType = "LIMIT"
	OrderTypeStop      OrderType = "STOP"
	OrderTypeStopLimit OrderType = "STOP_LIMIT"
)

// OrderStatus represents the current status of an order
type OrderStatus string

const (
	OrderStatusPending   OrderStatus = "PENDING"
	OrderStatusSubmitted OrderStatus = "SUBMITTED"
	OrderStatusPartial   OrderStatus = "PARTIAL"
	OrderStatusFilled    OrderStatus = "FILLED"
	OrderStatusCancelled OrderStatus = "CANCELLED"
	OrderStatusRejected  OrderStatus = "REJECTED"
	OrderStatusExpired   OrderStatus = "EXPIRED"
)

// TimeInForce represents how long an order remains active
type TimeInForce string

const (
	TIFGTC  TimeInForce = "GTC"  // Good Till Cancelled
	TIFIOC  TimeInForce = "IOC"  // Immediate Or Cancel
	TIFFOK  TimeInForce = "FOK"  // Fill Or Kill
	TIFGTD  TimeInForce = "GTD"  // Good Till Date
	TIFDAY  TimeInForce = "DAY"  // Day order
)

// Order represents a trading order
type Order struct {
	ID          string
	ClientOrderID string
	Symbol      string
	Side        Side
	Type        OrderType
	Status      OrderStatus
	Size        float64
	FilledSize  float64
	RemainingSize float64
	Price       float64
	StopPrice   float64
	TimeInForce TimeInForce
	ExpireTime  *time.Time
	SubmittedAt time.Time
	UpdatedAt   time.Time
	FilledAt    *time.Time
	AverageFillPrice float64
	Commission  float64
	Strategy    string
	Metadata    map[string]any
	Error       string
}

func NewOrder(symbol string, side Side, orderType OrderType, size float64, price, stopPrice float64) *Order {
	return &Order{
		ID:           util.GenerateIDWithPrefix("ord"),
		ClientOrderID: util.GenerateClientID(),
		Symbol:       symbol,
		Side:         side,
		Type:         orderType,
		Status:       OrderStatusPending,
		Size:         size,
		Price:        price,
		StopPrice:    stopPrice,
		TimeInForce:  TIFGTC,
		SubmittedAt:  time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		Metadata:     make(map[string]any),
	}
}

func (o *Order) IsActive() bool {
	return o.Status == OrderStatusPending || o.Status == OrderStatusSubmitted || o.Status == OrderStatusPartial
}

func (o *Order) IsTerminal() bool {
	return o.Status == OrderStatusFilled || o.Status == OrderStatusCancelled || o.Status == OrderStatusRejected || o.Status == OrderStatusExpired
}

func (o *Order) Fill(fillSize, fillPrice float64, commission float64) {
	o.FilledSize += fillSize
	o.RemainingSize = o.Size - o.FilledSize
	o.AverageFillPrice = ((o.AverageFillPrice * (o.FilledSize - fillSize)) + (fillPrice * fillSize)) / o.FilledSize
	o.Commission += commission
	o.UpdatedAt = time.Now().UTC()

	if o.RemainingSize <= 0.000001 {
		o.Status = OrderStatusFilled
		now := time.Now().UTC()
		o.FilledAt = &now
	} else {
		o.Status = OrderStatusPartial
	}
}

func (o *Order) Cancel() error {
	if !o.IsActive() {
		return errors.ErrInvalidInput
	}
	o.Status = OrderStatusCancelled
	o.UpdatedAt = time.Now().UTC()
	return nil
}

func (o *Order) Reject(reason string) {
	o.Status = OrderStatusRejected
	o.Error = reason
	o.UpdatedAt = time.Now().UTC()
}

func (o *Order) Remaining() float64 {
	return o.Size - o.FilledSize
}