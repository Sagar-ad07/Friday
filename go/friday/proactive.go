package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/friday-prototype/friday-go/pkg/db"
)

// Alert is a proactive notification from Friday to the user.
type Alert struct {
	ID        int64     `json:"id"`
	Type      string    `json:"type"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Severity  string    `json:"severity"`
	CreatedAt time.Time `json:"created_at"`
	Read      bool      `json:"read"`
}

const alertsDDL = `
CREATE TABLE IF NOT EXISTS alerts (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	type TEXT NOT NULL,
	title TEXT NOT NULL,
	body TEXT NOT NULL,
	severity TEXT NOT NULL DEFAULT 'info',
	created_at TEXT NOT NULL,
	read INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_alerts_read ON alerts(read);
CREATE INDEX IF NOT EXISTS idx_alerts_created ON alerts(created_at);
`

func init() {
	ensureAlertsTable()
}

func ensureAlertsTable() {
	defer func() { _ = recover() }()
	d := db.SafeGet()
	if d == nil {
		return
	}
	d.Exec(alertsDDL)
}

// CreateAlert stores a proactive alert.
func CreateAlert(alertType, title, body, severity string) {
	defer func() { _ = recover() }()
	d := db.SafeGet()
	if d != nil {
		d.Exec(
			`INSERT INTO alerts (type, title, body, severity, created_at) VALUES (?, ?, ?, ?, ?)`,
			alertType, title, body, severity, time.Now().UTC().Format(time.RFC3339),
		)
	}
	log.Printf("[ALERT] %s: %s", severity, title)

	// Speak critical/warning alerts aloud
	SpeakAlert(severity, title, body)
}

// GetUnreadAlerts returns alerts the user hasn't seen yet.
func GetUnreadAlerts() []Alert {
	defer func() { _ = recover() }()
	d := db.SafeGet()
	if d == nil {
		return nil
	}
	rows, err := d.Query(`SELECT id, type, title, body, severity, created_at FROM alerts WHERE read = 0 ORDER BY created_at DESC LIMIT 20`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var alerts []Alert
	for rows.Next() {
		var a Alert
		var ct string
		if err := rows.Scan(&a.ID, &a.Type, &a.Title, &a.Body, &a.Severity, &ct); err != nil {
			continue
		}
		a.CreatedAt, _ = time.Parse(time.RFC3339, ct)
		alerts = append(alerts, a)
	}
	return alerts
}

// MarkAlertsRead marks all alerts as read.
func MarkAlertsRead() {
	defer func() { _ = recover() }()
	d := db.SafeGet()
	if d == nil {
		return
	}
	d.Exec(`UPDATE alerts SET read = 1 WHERE read = 0`)
}

// ProactiveMonitor watches the trading bot and generates alerts.
// Runs as a goroutine — Friday's "always watching" instinct.
type ProactiveMonitor struct {
	lastDailyPnL   float64
	lastTotalPnL   float64
	lastTradesToday int
	lastTicket      int64
	checkInterval   time.Duration
	lastLinkScan    time.Time
}

func NewProactiveMonitor() *ProactiveMonitor {
	return &ProactiveMonitor{
		checkInterval: 30 * time.Second,
	}
}

// Start launches the proactive monitor goroutine.
func (pm *ProactiveMonitor) Start(ctx context.Context) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[PROACTIVE] monitor crashed: %v — restarting in 60s", r)
				time.Sleep(60 * time.Second)
				pm.Start(ctx)
			}
		}()

		log.Printf("[PROACTIVE] monitor started (checking every %v)", pm.checkInterval)
		ticker := time.NewTicker(pm.checkInterval)
		defer ticker.Stop()

		// Initial state
		pm.snapshotState()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pm.check()
			}
		}
	}()
}

func (pm *ProactiveMonitor) snapshotState() {
	pf := GetPropFirm()
	pf.mu.RLock()
	pm.lastDailyPnL = pf.DailyPnL
	pm.lastTotalPnL = pf.TotalPnL
	pm.lastTradesToday = pf.TradesToday
	pf.mu.RUnlock()
}

func (pm *ProactiveMonitor) check() {
	pf := GetPropFirm()
	pf.mu.RLock()
	currentDaily := pf.DailyPnL
	currentTotal := pf.TotalPnL
	currentTrades := pf.TradesToday
	tradingActive := pf.TradingActive
	pf.mu.RUnlock()

	// Trade closed — PnL changed
	if currentTrades > pm.lastTradesToday {
		pnlChange := currentTotal - pm.lastTotalPnL
		severity := "info"
		icon := "📊"
		if pnlChange > 0 {
			severity = "success"
			icon = "✅"
		} else if pnlChange < 0 {
			severity = "warning"
			icon = "❌"
		}
		CreateAlert(
			"trade_closed",
			fmt.Sprintf("%s Trade Closed — P&L: $%.2f", icon, pnlChange),
			fmt.Sprintf("Daily P&L: $%.2f | Total P&L: $%.2f | Trades today: %d\n\nRun generate_statement for full details.", currentDaily, currentTotal, currentTrades),
			severity,
		)
	}

	// Trading stopped — violation or target reached
	if !tradingActive && pm.lastTradesToday > 0 {
		pf.mu.RLock()
		lastErr := pf.LastError
		pf.mu.RUnlock()
		if lastErr != "" {
			CreateAlert(
				"trading_stopped",
				"🚫 Trading Stopped",
				lastErr,
				"critical",
			)
		}
	}

	// Daily loss warning — 75% of cap
	pf.mu.RLock()
	dailyCap := pf.Config.MaxDailyLoss
	pf.mu.RUnlock()
	if dailyCap > 0 && currentDaily < 0 {
		used := -currentDaily / dailyCap * 100
		if used >= 75 && used < 100 {
			CreateAlert(
				"loss_warning",
				"⚠️ Daily Loss Warning",
				fmt.Sprintf("You've used %.0f%% of your daily loss cap ($%.2f / $%.0f). Consider stopping for today.", used, -currentDaily, dailyCap),
				"warning",
			)
		}
	}

	// Update snapshot
	pm.lastDailyPnL = currentDaily
	pm.lastTotalPnL = currentTotal
	pm.lastTradesToday = currentTrades

	// Auto broken link scan — once per day
	if time.Since(pm.lastLinkScan) > 24*time.Hour {
		pm.lastLinkScan = time.Now()
		go pm.dailyLinkScan()
	}
}

func (pm *ProactiveMonitor) dailyLinkScan() {
	targets := []string{
		"https://blog.cloudflare.com",
		"https://go.dev/blog",
	}
	tool := &BrokenLinkTool{}
	for _, url := range targets {
		result, _ := tool.scan(url)
		if r, ok := result.(map[string]any); ok {
			if count, ok := r["broken_count"].(int); ok && count > 0 {
				log.Printf("[PROACTIVE] %d broken links found on %s", count, url)
				WriteAlertToFile(fmt.Sprintf("Broken links found: %s", url),
					fmt.Sprintf("Friday found %d broken links. Use broken_links scan to get details.", count))
			}
		}
	}
}

// WriteAlertToFile also writes critical alerts to a file for the UI to pick up.
func WriteAlertToFile(title, body string) {
	alertDir := filepath.Join(ProjectRoot, "data", "alerts")
	os.MkdirAll(alertDir, 0755)
	alertFile := filepath.Join(alertDir, "latest.json")
	alert := map[string]string{
		"title": title,
		"body":  body,
		"time":  time.Now().Format(time.RFC3339),
	}
	data, _ := json.MarshalIndent(alert, "", "  ")
	os.WriteFile(alertFile, data, 0644)
}

var _ = context.Background