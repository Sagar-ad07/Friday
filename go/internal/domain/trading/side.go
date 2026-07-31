package trading

// Side represents the direction of a trade
type Side string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

func (s Side) String() string { return string(s) }

func (s Side) Opposite() Side {
	if s == SideBuy {
		return SideSell
	}
	return SideBuy
}

func (s Side) Multiplier() float64 {
	if s == SideBuy {
		return 1
	}
	return -1
}

func ParseSide(s string) (Side, bool) {
	switch s {
	case "BUY", "buy", "Buy", "LONG", "long":
		return SideBuy, true
	case "SELL", "sell", "Sell", "SHORT", "short":
		return SideSell, true
	}
	return "", false
}