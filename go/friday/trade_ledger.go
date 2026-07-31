package friday

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/friday-prototype/friday-go/pkg/db"
)

// TradeRecord holds one executed trade for the prop firm ledger.
type TradeRecord struct {
	ID         int64     `json:"id"`
	Ticket     int64     `json:"ticket"`
	Symbol     string    `json:"symbol"`
	Direction  string    `json:"direction"`
	Lots       float64   `json:"lots"`
	OpenPrice  float64   `json:"open_price"`
	ClosePrice float64   `json:"close_price"`
	OpenTime   time.Time `json:"open_time"`
	CloseTime  time.Time `json:"close_time"`
	PnL        float64   `json:"pnl"`
	Tags       string    `json:"tags,omitempty"`
}

const tradeLedgerDDL = `
	CREATE TABLE IF NOT EXISTS trade_ledger (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ticket INTEGER NOT NULL,
		symbol TEXT NOT NULL,
		direction TEXT NOT NULL,
		lots REAL NOT NULL,
		open_price REAL NOT NULL,
		close_price REAL NOT NULL,
		open_time TEXT NOT NULL,
		close_time TEXT NOT NULL,
		pnl REAL NOT NULL,
		tags TEXT DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS idx_trade_ledger_close ON trade_ledger(close_time);
	CREATE INDEX IF NOT EXISTS idx_trade_ledger_ticket ON trade_ledger(ticket);
`

func init() {
	ensureTradeLedgerTable()
}

func ensureTradeLedgerTable() {
	d := db.SafeGet()
	if d == nil {
		return
	}
	if _, err := d.Exec(tradeLedgerDDL); err != nil {
		log.Printf("Trade ledger table: %v", err)
	}
}

// RecordTradeToLedger persists a completed trade to SQLite.
// Called by the engine's RecordTrade callback when a ticket closes.
func RecordTradeToLedger(ticket int64, symbol, direction string, lots, openPrice, closePrice float64, openTime, closeTime time.Time, pnl float64) error {
	if ticket == 0 || closeTime.IsZero() {
		return nil
	}
	_, err := db.Get().Exec(
		`INSERT INTO trade_ledger (ticket, symbol, direction, lots, open_price, close_price, open_time, close_time, pnl, tags)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ticket, symbol, direction, lots, openPrice, closePrice,
		openTime.UTC().Format(time.RFC3339),
		closeTime.UTC().Format(time.RFC3339),
		pnl, "propfirm",
	)
	if err != nil {
		return fmt.Errorf("ledger insert: %w", err)
	}
	log.Printf("[LEDGER] trade %d %s %s %.2f lots $%.2f", ticket, symbol, direction, lots, pnl)
	return nil
}

// GenerateStatement returns a prop firm–ready trade statement for a date range.
// Format matches what prop firms expect: trade list + summary stats.
func GenerateStatement(ctx context.Context, from, to time.Time) (string, error) {
	rows, err := db.Get().QueryContext(ctx,
		`SELECT ticket, symbol, direction, lots, open_price, close_price, open_time, close_time, pnl
		 FROM trade_ledger
		 WHERE close_time >= ? AND close_time < ?
		 ORDER BY close_time ASC`,
		from.UTC().Format(time.RFC3339),
		to.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return "", fmt.Errorf("statement query: %w", err)
	}
	defer rows.Close()

	var trades []TradeRecord
	for rows.Next() {
		var t TradeRecord
		var ot, ct string
		if err := rows.Scan(&t.Ticket, &t.Symbol, &t.Direction, &t.Lots,
			&t.OpenPrice, &t.ClosePrice, &ot, &ct, &t.PnL); err != nil {
			continue
		}
		t.OpenTime, _ = time.Parse(time.RFC3339, ot)
		t.CloseTime, _ = time.Parse(time.RFC3339, ct)
		trades = append(trades, t)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	if len(trades) == 0 {
		return "No trades closed in this period.", nil
	}

	var sb strings.Builder
	sb.WriteString("PROP FIRM TRADE STATEMENT\n")
	sb.WriteString(strings.Repeat("=", 60) + "\n")
	sb.WriteString(fmt.Sprintf("Period: %s – %s\n", from.Format("2006-01-02"), to.Format("2006-01-02")))
	sb.WriteString(fmt.Sprintf("Total Trades: %d\n", len(trades)))
	sb.WriteString("\n")

	// Per-day breakdown
	type daySummary struct {
		date   string
		count  int
		pnl    float64
		wins   int
		losses int
	}
	days := make(map[string]*daySummary)
	var dates []string

	totalWins := 0
	totalLosses := 0
	totalPnL := 0.0

	sb.WriteString(fmt.Sprintf("%-5s %-7s %-4s %-9s %-9s %-10s %s\n",
		"#", "Ticket", "Dir", "Lots", "Open", "Close", "PnL"))
	sb.WriteString(strings.Repeat("-", 60) + "\n")

	for i, t := range trades {
		sb.WriteString(fmt.Sprintf("%-5d %-7d %-4s %-9.2f %-9.5f %-9.5f $%.2f\n",
			i+1, t.Ticket, t.Direction, t.Lots, t.OpenPrice, t.ClosePrice, t.PnL))

		totalPnL += t.PnL
		if t.PnL > 0 {
			totalWins++
		} else {
			totalLosses++
		}

		dayKey := t.CloseTime.Format("2006-01-02")
		if _, ok := days[dayKey]; !ok {
			days[dayKey] = &daySummary{date: dayKey}
			dates = append(dates, dayKey)
		}
		ds := days[dayKey]
		ds.count++
		ds.pnl += t.PnL
		if t.PnL > 0 {
			ds.wins++
		} else {
			ds.losses++
		}
	}

	sb.WriteString(strings.Repeat("=", 60) + "\n")

	// Summary stats
	winRate := 0.0
	if len(trades) > 0 {
		winRate = float64(totalWins) / float64(len(trades)) * 100
	}

	sb.WriteString("\n=== PERFORMANCE SUMMARY ===\n")
	sb.WriteString(fmt.Sprintf("Total P&L:       $%.2f\n", totalPnL))
	sb.WriteString(fmt.Sprintf("Win Rate:        %.1f%% (%d/%d)\n", winRate, totalWins, len(trades)))
	sb.WriteString(fmt.Sprintf("Avg Win:         $%.2f\n", avgOf(trades, true)))
	sb.WriteString(fmt.Sprintf("Avg Loss:        $%.2f\n", avgOf(trades, false)))
	sb.WriteString(fmt.Sprintf("Profit Factor:   %.2f\n", profitFactor(trades)))
	sb.WriteString(fmt.Sprintf("Trading Days:    %d\n", len(dates)))
	sb.WriteString("\n")

	// Daily breakdown
	sb.WriteString("=== DAILY BREAKDOWN ===\n")
	sb.WriteString(fmt.Sprintf("%-12s %-5s %-10s %-5s %-5s\n", "Date", "Trades", "PnL", "W", "L"))
	sb.WriteString(strings.Repeat("-", 42) + "\n")
	for _, d := range dates {
		ds := days[d]
		sb.WriteString(fmt.Sprintf("%-12s %-5d $%-8.2f %-5d %-5d\n", ds.date, ds.count, ds.pnl, ds.wins, ds.losses))
	}

	// Consistency check
	if totalPnL > 0 && len(dates) > 1 {
		maxDay := 0.0
		for _, d := range dates {
			if days[d].pnl > maxDay {
				maxDay = days[d].pnl
			}
		}
		consistency := maxDay / totalPnL * 100
		sb.WriteString(fmt.Sprintf("\nConsistency: Best day $%.2f = %.1f%% of total\n", maxDay, consistency))
		if consistency <= 15 {
			sb.WriteString("✓ PASSES consistency rule (≤ 15%)\n")
		} else {
			sb.WriteString(fmt.Sprintf("✗ FAILS consistency rule (%.1f%% > 15%%)\n", consistency))
		}
	}

	return sb.String(), nil
}

func avgOf(trades []TradeRecord, wins bool) float64 {
	var total float64
	var count int
	for _, t := range trades {
		if wins && t.PnL > 0 {
			total += t.PnL
			count++
		} else if !wins && t.PnL <= 0 {
			total += t.PnL
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func profitFactor(trades []TradeRecord) float64 {
	var grossWin, grossLoss float64
	for _, t := range trades {
		if t.PnL > 0 {
			grossWin += t.PnL
		} else {
			grossLoss += -t.PnL
		}
	}
	if grossLoss == 0 {
		return 0
	}
	return grossWin / grossLoss
}

// GetTradeLedger returns recent trades for the AI tool.
func GetTradeLedger(ctx context.Context, limit int) ([]TradeRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	rows, err := db.Get().QueryContext(ctx,
		`SELECT id, ticket, symbol, direction, lots, open_price, close_price, open_time, close_time, pnl, tags
		 FROM trade_ledger ORDER BY close_time DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var trades []TradeRecord
	for rows.Next() {
		var t TradeRecord
		var ot, ct string
		if err := rows.Scan(&t.ID, &t.Ticket, &t.Symbol, &t.Direction, &t.Lots,
			&t.OpenPrice, &t.ClosePrice, &ot, &ct, &t.PnL, &t.Tags); err != nil {
			continue
		}
		t.OpenTime, _ = time.Parse(time.RFC3339, ot)
		t.CloseTime, _ = time.Parse(time.RFC3339, ct)
		trades = append(trades, t)
	}
	return trades, rows.Err()
}

var _ = sql.ErrNoRows