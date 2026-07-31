package friday

import (
	"log"
	"sync/atomic"
	"time"
)

// RevolutionTradingSystem is the investment-recovery overlay: it watches the
// live account state and reports recovery progress. It is intentionally thin
// right now — the heavy lifting lives in the trading engine — so it only
// tracks startup time and a running recovery score for the dashboard.
type RevolutionTradingSystem struct {
	startedAt     time.Time
	recoveryScore atomic.Int64
}

// NewRevolutionTradingSystem creates the recovery tracker.
func NewRevolutionTradingSystem() *RevolutionTradingSystem {
	s := &RevolutionTradingSystem{startedAt: time.Now()}
	log.Println("[REVTRADING] investment recovery system armed")
	return s
}

// StartedAt returns when the system came up.
func (rts *RevolutionTradingSystem) StartedAt() time.Time {
	return rts.startedAt
}

// SetRecoveryScore updates the current recovery progress (0-100).
func (rts *RevolutionTradingSystem) SetRecoveryScore(score int64) {
	rts.recoveryScore.Store(score)
}

// RecoveryScore returns the latest recovery progress.
func (rts *RevolutionTradingSystem) RecoveryScore() int64 {
	return rts.recoveryScore.Load()
}

// Snapshot returns a dashboard-friendly view of the recovery system.
func (rts *RevolutionTradingSystem) Snapshot() map[string]any {
	return map[string]any{
		"started_at":     rts.startedAt.Format(time.RFC3339),
		"recovery_score": rts.recoveryScore.Load(),
		"status":         "armed",
	}
}
