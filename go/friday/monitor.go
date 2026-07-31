package friday

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// monitorState holds the last-known snapshot for diffing, keyed by
// "field" so we only emit an activity event on a real TRANSITION.
type monitorState struct {
	mu sync.RWMutex

	// latched flags (avoid spamming the same event every poll)
	bgRunning       *bool   // nil until first seen
	bgHaltedNotified bool
	capReachedNotif  bool
	balance         *float64
	balanceNotified map[string]bool // "closed" trade tickets we've announced
	lastExnessPos   []int64         // last-seen ticket set
}

// TradingSnapshot is the combined live view for the UI.
type TradingSnapshot struct {
	TS        time.Time        `json:"ts"`
	BG        map[string]any   `json:"bg"`
	Exness    map[string]any   `json:"exness"`
	Positions []map[string]any `json:"positions"`
	Account   map[string]any   `json:"account"`
	PropFirm  map[string]any   `json:"prop_firm"`
	Version   int              `json:"version"`
}

var (
	monitorOnce sync.Once
	monState    *monitorState
	monSnapshot TradingSnapshot
	monVersion  int
	monCacheMu  sync.RWMutex
	monLastFetch time.Time
)

// StartTradingMonitor launches a background goroutine that polls the
// trading engine (8001) for BG + Exness state every interval, diffs it,
// and publishes activity events on meaningful transitions only.
func StartTradingMonitor(engineURL string, interval time.Duration) {
	monitorOnce.Do(func() {
		monState = &monitorState{
			balanceNotified: make(map[string]bool),
			lastExnessPos:   []int64{},
		}
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			fetchAndDiff(engineURL) // initial
			for range ticker.C {
				fetchAndDiff(engineURL)
			}
		}()
		log.Printf("[MONITOR] trading monitor started (interval=%v, engine=%s)", interval, engineURL)
	})
}

func fetchAndDiff(engineURL string) {
	client := &http.Client{Timeout: 4 * time.Second}

	type result struct {
		key  string
		data map[string]any
		err  error
	}
	ch := make(chan result, 5)
	go func() { d, e := fetchJSON(client, engineURL+"/trading/status"); ch <- result{"bg", d, e} }()
	go func() { d, e := fetchJSON(client, engineURL+"/trading/exness/status"); ch <- result{"exness", d, e} }()
	go func() { d, e := fetchJSON(client, engineURL+"/mt5/exness/positions"); ch <- result{"exness_pos", d, e} }()
	go func() { d, e := fetchJSON(client, engineURL+"/mt5/exness/account"); ch <- result{"exness_acct", d, e} }()
	go func() { d, e := fetchJSON(client, engineURL+"/trading/propfirm"); ch <- result{"propfirm", d, e} }()

	results := make(map[string]map[string]any, 5)
	for i := 0; i < 5; i++ {
		r := <-ch
		if r.err != nil {
			continue
		}
		results[r.key] = r.data
	}

	publishDiff(results)
	updateSnapshot(results)
}

func fetchJSON(client *http.Client, url string) (map[string]any, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func publishDiff(results map[string]map[string]any) {
	monState.mu.Lock()
	defer monState.mu.Unlock()

	bg := results["bg"]
	posData := results["exness_pos"]
	acct := results["exness_acct"]
	_ = results["exness"] // tracked in snapshot only

	// BG halt transition: only publish when running flips false (or on first sight).
	if bg != nil {
		running, _ := bg["running"].(bool)
		errStr, _ := bg["last_error"].(string)
		if !running {
			if monState.bgRunning == nil || *monState.bgRunning {
				// transition to halted
				msg := "bot halted"
				if errStr != "" {
					msg = errStr
				}
				PublishActivity("system", "BlueGuardian halted", msg)
				if cap, _ := bg["daily_profit_cap"].(float64); cap > 0 {
					if pnl, _ := bg["daily_pnl"].(float64); pnl >= cap {
						PublishActivity("trade", "BG cap reached", fmt.Sprintf("daily PnL %.2f >= cap %.2f — trading paused", pnl, cap))
					}
				}
			}
		}
		if monState.bgRunning == nil {
			monState.bgRunning = &running
		} else if running != *monState.bgRunning {
			*monState.bgRunning = running
		}
	}

	// Exness trade open/close via position set diff.
	var curPos []int64
	if posData != nil {
		if pos, ok := posData["positions"].([]any); ok {
			for _, p := range pos {
				if m, ok := p.(map[string]any); ok {
					if t, ok := m["ticket"].(float64); ok {
						tid := int64(t)
						curPos = append(curPos, tid)
						if !containsID(monState.lastExnessPos, tid) {
							dir := m["type"]
							vol := m["volume"]
							PublishActivity("trade", "Exness trade opened",
								fmt.Sprintf("%s %.2f lots ticket=%d", dir, vol, tid))
						}
					}
				}
			}
		}
	}
	// Detect closed positions.
	for _, oldT := range monState.lastExnessPos {
		if !containsID(curPos, oldT) {
			key := fmt.Sprintf("closed:%d", oldT)
			if !monState.balanceNotified[key] {
				PublishActivity("trade", "Exness trade closed", fmt.Sprintf("ticket=%d resolved", oldT))
				monState.balanceNotified[key] = true
			}
		}
	}
	monState.lastExnessPos = curPos

	// Exness balance change.
	if acct != nil {
		if bal, ok := acct["balance"].(float64); ok {
			if monState.balance != nil && bal != *monState.balance && !monState.balanceNotified[fmt.Sprintf("bal:%d", int(bal*100))] {
				dir := "↑"
				if bal < *monState.balance {
					dir = "↓"
				}
				PublishActivity("trade", "Exness balance", fmt.Sprintf("%s %.2f AED (now %.2f %s)", dir, bal-*monState.balance, bal, acct["currency"]))
				monState.balanceNotified[fmt.Sprintf("bal:%d", int(bal*100))] = true
			}
			if monState.balance == nil {
				monState.balance = &bal
			} else if bal != *monState.balance {
				*monState.balance = bal
				// reset balance-notified on next change
			}
			if bal < 10.0 {
				PublishActivity("system", "Exness low balance", fmt.Sprintf("balance %.2f %s < AED 10", bal, acct["currency"]))
			}
		}
	}
}

func containsID(ids []int64, id int64) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func updateSnapshot(results map[string]map[string]any) {
	monCacheMu.Lock()
	defer monCacheMu.Unlock()
	monVersion++
	var positions []map[string]any
	if d := results["exness_pos"]; d != nil {
		if pos, ok := d["positions"].([]any); ok {
			for _, p := range pos {
				if m, ok := p.(map[string]any); ok {
					positions = append(positions, m)
				}
			}
		}
	}
	monSnapshot = TradingSnapshot{
		TS:        time.Now(),
		BG:        results["bg"],
		Exness:    results["exness"],
		Positions: positions,
		Account:   results["exness_acct"],
		PropFirm:  results["propfirm"],
		Version:   monVersion,
	}
	monLastFetch = time.Now()
}

// GetMonitorSnapshot returns the cached trading snapshot (freshness <= 10s).
func GetMonitorSnapshot() TradingSnapshot {
	monCacheMu.RLock()
	defer monCacheMu.RUnlock()
	return monSnapshot
}

// MonitorSnapshotHandler serves the combined snapshot for the UI.
func (s *Server) MonitorSnapshotHandler(c *gin.Context) {
	c.JSON(http.StatusOK, GetMonitorSnapshot())
}
