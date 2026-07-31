package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ────────────────────────────────────────────────────────────────────────────
// Live state — the single source of truth for "what is Friday doing right now".
// Everything here is fetched from the trading engine and cached briefly.
// No hardcoded balances, no invented win rates: only what the engine says.
// ────────────────────────────────────────────────────────────────────────────

// EnginePort is where the trading engine (MT5 bot) listens.
func EnginePort() string {
	if p := os.Getenv("TRADING_ENGINE_PORT"); p != "" {
		return p
	}
	return "8001"
}

// EngineBaseURL returns the trading engine root URL.
func EngineBaseURL() string {
	return "http://localhost:" + EnginePort()
}

// LiveAccount mirrors the engine's /mt5/account response.
type LiveAccount struct {
	Login    int     `json:"login"`
	Server   string  `json:"server"`
	Balance  float64 `json:"balance"`
	Equity   float64 `json:"equity"`
	Currency string  `json:"currency"`
	Leverage int     `json:"leverage"`
	Profit   float64 `json:"profit"`
	Margin   float64 `json:"margin"`
}

// LivePosition mirrors one entry of the engine's /mt5/positions response.
type LivePosition struct {
	Ticket       int64   `json:"ticket"`
	Symbol       string  `json:"symbol"`
	Type         string  `json:"type"`
	Volume       float64 `json:"volume"`
	PriceOpen    float64 `json:"price_open"`
	PriceCurrent float64 `json:"price_current"`
	SL           float64 `json:"sl"`
	TP           float64 `json:"tp"`
	Profit       float64 `json:"profit"`
	Magic        int64   `json:"magic"`
}

// LiveSnapshot is the truthful picture of the account at fetch time.
type LiveSnapshot struct {
	Account     *LiveAccount   `json:"account,omitempty"`
	Positions   []LivePosition `json:"positions"`
	EngineAlive bool           `json:"engine_alive"`
	FetchedAt   time.Time      `json:"fetched_at"`
}

// liveCache holds the most recent snapshot for a few seconds so chat and
// status handlers don't hammer the engine on every request.
type liveCacheT struct {
	mu     sync.Mutex
	snap   *LiveSnapshot
	at     time.Time
}

var liveCache liveCacheT

const liveTTL = 5 * time.Second

// fetchEngineJSON GETs url into v. Returns an error if the engine is down.
func fetchEngineJSON(ctx context.Context, url string, v any) error {
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, "GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("engine replied %d on %s", resp.StatusCode, url)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// LiveState fetches (or reuses a fresh) snapshot of the live account and
// open positions. Errors are folded into the snapshot so callers can always
// render something truthful: "engine unreachable" instead of stale numbers.
func LiveState(ctx context.Context) *LiveSnapshot {
	liveCache.mu.Lock()
	defer liveCache.mu.Unlock()

	if liveCache.snap != nil && time.Since(liveCache.at) < liveTTL {
		return liveCache.snap
	}

	snap := &LiveSnapshot{FetchedAt: time.Now()}

	var account LiveAccount
	if err := fetchEngineJSON(ctx, EngineBaseURL()+"/mt5/account", &account); err != nil {
		snap.EngineAlive = false
	} else {
		snap.EngineAlive = true
		snap.Account = &account
	}

	var posResp struct {
		Positions []LivePosition `json:"positions"`
	}
	if err := fetchEngineJSON(ctx, EngineBaseURL()+"/mt5/positions", &posResp); err == nil {
		snap.Positions = posResp.Positions
	}

	liveCache.snap = snap
	liveCache.at = time.Now()
	return snap
}

// HasTradingIntent reports whether the user's question is about the live
// account, open trades, or the trading bot — the cases where Friday must
// answer from the engine, not from memory.
func HasTradingIntent(text string) bool {
	t := strings.ToLower(text)
	needles := []string{
		"balance", "equity", "margin", "leverage", "account", "login",
		"position", "ticket", "profit", "pnl", "drawdown", "lot",
		"open trade", "closed trade", "bot status", "trading status",
		"mt5", "blue guardian", "exness", "grid", "propfirm", "prop firm",
		"trade", "trading", "order", "stop loss", "take profit",
	}
	for _, n := range needles {
		if strings.Contains(t, n) {
			return true
		}
	}
	return false
}

// LiveContextBlock renders the snapshot as compact, verifiable facts for
// injection into a system prompt. Every number comes from the engine.
func (s *LiveSnapshot) LiveContextBlock() string {
	if s == nil || s.Account == nil {
		return "[Live state: trading engine unreachable — do not claim any balance, position, or win-rate. Say so if asked.]"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "[Live account (from engine, fetched %s): balance %.2f %s, equity %.2f, profit %.2f, margin %.2f, leverage 1:%d, login %d @ %s.",
		s.FetchedAt.Format("15:04:05"), s.Account.Balance, s.Account.Currency,
		s.Account.Equity, s.Account.Profit, s.Account.Margin, s.Account.Leverage,
		s.Account.Login, s.Account.Server)

	if len(s.Positions) == 0 {
		sb.WriteString(" No open positions.]")
		return sb.String()
	}

	sb.WriteString(" Open positions:")
	for _, p := range s.Positions {
		fmt.Fprintf(&sb, " %s %s %.2f lots @ %.5f (now %.5f, profit %+.2f %s)",
			strings.ToUpper(p.Type), p.Symbol, p.Volume, p.PriceOpen, p.PriceCurrent, p.Profit, s.Account.Currency)
	}
	sb.WriteString("]")
	return sb.String()
}

// LiveContextHuman renders the snapshot as natural prose for direct answers
// (the /command status path and local chat replies).
func (s *LiveSnapshot) LiveContextHuman() string {
	if s == nil || s.Account == nil {
		return "Trading engine is unreachable right now — I can't see the live account. It may be restarting."
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Live account on %s (login %d, leverage 1:%d): balance %.2f %s, equity %.2f, floating profit %+.2f %s, margin in use %.2f.",
		s.Account.Server, s.Account.Login, s.Account.Leverage,
		s.Account.Balance, s.Account.Currency, s.Account.Equity,
		s.Account.Profit, s.Account.Currency, s.Account.Margin)

	if len(s.Positions) == 0 {
		sb.WriteString("\nNo open positions.")
		return sb.String()
	}

	sb.WriteString("\nOpen positions:")
	for _, p := range s.Positions {
		side := "buying"
		if p.Type == "sell" {
			side = "selling"
		}
		fmt.Fprintf(&sb, "\n  - %s %.2f lots of %s opened @ %.5f, last price %.5f — %+.2f %s",
			side, p.Volume, p.Symbol, p.PriceOpen, p.PriceCurrent, p.Profit, s.Account.Currency)
	}
	return sb.String()
}

// tradingIntentOf detects which slice of the snapshot a query is after.
// Used to answer "what's my balance" style questions with one relevant line.
func tradingIntentOf(text string) string {
	t := strings.ToLower(text)
	switch {
	case strings.Contains(t, "position") || strings.Contains(t, "open trade") || strings.Contains(t, "ticket") || strings.Contains(t, "lot"):
		return "positions"
	case strings.Contains(t, "profit") || strings.Contains(t, "pnl"):
		return "profit"
	case strings.Contains(t, "equity") || strings.Contains(t, "margin"):
		return "equity"
	case strings.Contains(t, "balance") || strings.Contains(t, "account"):
		return "account"
	default:
		return "all"
	}
}
