package trading

// RiskParams holds risk management parameters
type RiskParams struct {
	MaxDailyLoss       float64
	MaxDrawdownPercent float64
	MaxPositionSizePct float64
	MinConsistency     float64
	MinHoldingTimeSec  int
	MaxTradesPerHour   int
	MaxSpreadPips      float64
	MinConfidence      float64
	CooldownAfterLoss  int // seconds
}

func DefaultRiskParams() *RiskParams {
	return &RiskParams{
		MaxDailyLoss:       150.0,
		MaxDrawdownPercent: 20.0,
		MaxPositionSizePct: 10.0,
		MinConsistency:     15.0,
		MinHoldingTimeSec:  60,
		MaxTradesPerHour:   120,
		MaxSpreadPips:      0.5,
		MinConfidence:      0.85,
		CooldownAfterLoss:  300,
	}
}

func (r *RiskParams) Validate() error {
	if r.MaxDailyLoss <= 0 {
		return ErrInvalidRiskParam("max_daily_loss must be positive")
	}
	if r.MaxDrawdownPercent <= 0 || r.MaxDrawdownPercent > 100 {
		return ErrInvalidRiskParam("max_drawdown_percent must be 0-100")
	}
	if r.MaxPositionSizePct <= 0 || r.MaxPositionSizePct > 100 {
		return ErrInvalidRiskParam("max_position_size_pct must be 0-100")
	}
	if r.MinConsistency < 0 || r.MinConsistency > 100 {
		return ErrInvalidRiskParam("min_consistency must be 0-100")
	}
	if r.MinHoldingTimeSec < 0 {
		return ErrInvalidRiskParam("min_holding_time_sec must be non-negative")
	}
	if r.MaxTradesPerHour <= 0 {
		return ErrInvalidRiskParam("max_trades_per_hour must be positive")
	}
	return nil
}

type riskParamError string

func (e riskParamError) Error() string { return string(e) }

func ErrInvalidRiskParam(msg string) error { return riskParamError("invalid risk param: " + msg) }