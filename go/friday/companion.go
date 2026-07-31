package friday

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// CompanionState holds persistent user state so Friday never forgets.
type CompanionState struct {
	mu             sync.RWMutex
	path           string
	UserID         string            `json:"user_id"`
	UserName       string            `json:"user_name,omitempty"`
	FirstSeen      time.Time         `json:"first_seen"`
	LastSeen       time.Time         `json:"last_seen"`
	TotalMessages  int               `json:"total_messages"`
	Preferences    map[string]string `json:"preferences,omitempty"`
	CrashCount     int               `json:"crash_count"`
	LastCrash      time.Time         `json:"last_crash,omitempty"`
	LastBackup     time.Time         `json:"last_backup,omitempty"`
	AutoRestart    bool              `json:"auto_restart"`
	Capabilities   map[string]interface{} `json:"capabilities,omitempty"`
	ChatHistory    []map[string]string `json:"chat_history,omitempty"` // Last 20 messages for memory
}

var (
	globalState *CompanionState
	stateOnce   sync.Once
)

// GetCompanionState returns the singleton companion state.
func GetCompanionState() *CompanionState {
	stateOnce.Do(func() {
		dir := filepath.Join(ProjectRoot, "data", "state")
		os.MkdirAll(dir, 0755)
		path := filepath.Join(dir, "companion.json")

		cs := &CompanionState{
			path:        path,
			UserID:      "default",
			FirstSeen:   time.Now(),
			LastSeen:    time.Now(),
			Preferences: make(map[string]string),
			AutoRestart: true,
		}

		// Try to load existing state
		if data, err := os.ReadFile(path); err == nil {
			if err := json.Unmarshal(data, cs); err == nil {
				log.Printf("Companion state loaded: user since %s, %d messages, %d capabilities",
					cs.FirstSeen.Format("2006-01-02"), cs.TotalMessages, len(cs.Capabilities))
			}
		}

		if cs.Capabilities == nil {
			cs.Capabilities = make(map[string]interface{})
		}

		cs.LastSeen = time.Now()
		globalState = cs
		cs.registerBots()
		cs.Save() // persist immediately
	})
	return globalState
}

// Save persists companion state to disk.
func (cs *CompanionState) Save() {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	data, err := json.MarshalIndent(cs, "", "  ")
	if err != nil {
		log.Printf("Companion state marshal error: %v", err)
		return
	}

	tmpPath := cs.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		log.Printf("Companion state save error: %v", err)
		return
	}
	os.Rename(tmpPath, cs.path)
}

// RecordMessage increments message count and saves.
func (cs *CompanionState) RecordMessage(role, content string) {
	cs.mu.Lock()
	cs.TotalMessages++
	cs.LastSeen = time.Now()
	if cs.ChatHistory == nil {
		cs.ChatHistory = make([]map[string]string, 0, 20)
	}
	cs.ChatHistory = append(cs.ChatHistory, map[string]string{"role": role, "content": content})
	if len(cs.ChatHistory) > 20 {
		cs.ChatHistory = cs.ChatHistory[len(cs.ChatHistory)-20:]
	}
	cs.mu.Unlock()
	cs.Save()
}

func (cs *CompanionState) GetHistory() []map[string]string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	if cs.ChatHistory == nil {
		return nil
	}
	return cs.ChatHistory
}

// RecordCrash increments crash counter and saves.
func (cs *CompanionState) RecordCrash() {
	cs.mu.Lock()
	cs.CrashCount++
	cs.LastCrash = time.Now()
	cs.mu.Unlock()
	cs.Save()
}

// SetPreference stores a user preference.
func (cs *CompanionState) SetPreference(key, value string) {
	cs.mu.Lock()
	cs.Preferences[key] = value
	cs.mu.Unlock()
	cs.Save()
}

// GetPreference retrieves a user preference.
func (cs *CompanionState) GetPreference(key string) string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.Preferences[key]
}

// Summary returns a friendly summary of the companion.
func (cs *CompanionState) Summary() string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	since := time.Since(cs.FirstSeen)
	days := int(since.Hours() / 24)
	if days < 1 {
		days = 1
	}

	return "Been together " + pluralize(days, "day") + ", " +
		pluralize(cs.TotalMessages, "message") + " shared."
}

func pluralize(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return itoa(n) + " " + noun + "s"
}

func (cs *CompanionState) SetCapability(name string, info interface{}) {
	cs.mu.Lock()
	if cs.Capabilities == nil {
		cs.Capabilities = make(map[string]interface{})
	}
	cs.Capabilities[name] = info
	cs.mu.Unlock()
	cs.Save()
}

func (cs *CompanionState) GetCapability(name string) (interface{}, bool) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	v, ok := cs.Capabilities[name]
	return v, ok
}

func (cs *CompanionState) AllCapabilities() map[string]interface{} {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	c := make(map[string]interface{})
	for k, v := range cs.Capabilities {
		c[k] = v
	}
	return c
}

func (cs *CompanionState) registerBots() {
	now := time.Now().UTC().Format(time.RFC3339)

	// Truthful capability catalog. The previous version hardcoded five
	// bots as "status":"active" with fabricated account sizes ($50k BG when
	// the real PropFirmConfig is $5k) and a fabricated strategy list. These
	// entries are descriptions only — actual run status is fetched live
	// via the check_all_bots / exness_bot / blue_guardian_bot tools.

	cs.Capabilities["mt5_swing_bot"] = map[string]interface{}{
		"status":      "see check_all_bots tool", // not hardcoded active
		"description": "Single MT5 connection to whichever account the engine is currently logged into (one terminal64.exe per friday.exe). Engine strategy: BB-RSI + EMA-9 cross + dormant London-ORB by hour. P2 alignment: bot.symbol='EURUSD' (was EURUSDm — that suffix isn't streamed by BlueGuardian), price feed is M30 candles (was M1 scalp). On propfirm accounts (BlueGuardian-Server) the executor overrides SL/TP to fixed 50pip SL / 100pip TP from the actual fill price (broker-accurate). Personal accounts (Exness-MT5Real3) keep the strategy's std-based SL/TP — no swing restriction per user directive.",
		"hardcoded_symbol_in_bot": "EURUSD",
		"timeframe_in_bot":        "M30",
		"sl_tp_profile":           "propfirm: 50pip SL / 100pip TP fixed; personal: strategy std-based",
		"compliance": map[string]any{
			"source":            "PropFirmConfig (seeds $5k / $150 / 5%% / 15%% / $250 target)",
			"account_size":     5000,
			"daily_loss_limit": 150,
			"max_drawdown_pct": 5.0,
			"profit_target":    250,
			"consistency_pct":  15.0,
		},
		"pnl_tracking":  "live via positionMonitor — closed tickets realized through HistoryDealsGet and pushed to PropFirmState.RecordTrade; tracked_tickets persist across restarts.",
		"built_at":      now,
	}

	// Keep the legacy 'exness_bot' and 'blue_guardian_bot' keys as
	// descriptions so existing capability callers don't break, but
	// mark them as aliases — not separate bots. There is only ONE MT5
	// connection; 'Exness' vs 'Blue Guardian' is just which account is
	// logged into that one terminal, governed by manage_accounts.
	cs.Capabilities["exness_bot"] = map[string]interface{}{
		"status":      "alias of mt5_swing_bot", // not hardcoded active
		"description": "Alias for the active MT5 bot. The Exness Private account (login 167036042 @ Exness-MT5Real3) is configured in AccountManager seed data but is DORMANT — there is no code that spawns a second gomt5.Client for it. Switching to it requires the trading engine to reconnect, which is not currently implemented.",
		"built_at":    now,
	}

	cs.Capabilities["blue_guardian_bot"] = map[string]interface{}{
		"status":        "alias of mt5_swing_bot", // not hardcoded active
		"description":   "Alias for the active MT5 bot when connected to BlueGuardian-Server. The currently seeded PropFirmConfig ('$5k Instant Starter') matches this account; the BG seed is the ACTIVE default. The $50,000 BG figure previously reported here was fabricated and did NOT match the real PropFirmConfig ($5,000).",
		"built_at":      now,
	}

	cs.Capabilities["passive_income"] = map[string]interface{}{
		"status":      "see passive_income tool",
		"type":        "passive income",
		"built_at":    now,
		"description": "Autonomous passive income setup. Friday discovers and configures income sources like bandwidth sharing (Honeygain). Use passive_income tool with action=discover to find available sources, action=setup to configure them.",
	}

	cs.Capabilities["compounder"] = map[string]interface{}{
		"status":        "see compounder tool",
		"built_at":      now,
		"auto_reinvest": true,
		"description":   "Capital compounding engine across all live streams. Probes the engine :8001 for live state.",
	}

	cs.Capabilities["self_healer"] = map[string]interface{}{
		"status":         "see self_healer tool",
		"built_at":       now,
		"auto_heal":      true,
		"max_retries":    3,
		"description":    "Self-repair system. Check via self_healer tool for live health.",
	}

	cs.Capabilities["resource_manager"] = map[string]interface{}{
		"status":           "see resource_manager tool",
		"built_at":         now,
		"laptop_ram_gb":    16,
		"laptop_ssd_gb":    256,
		"mining_max_pct":   40,
		"description":      "Laptop resource monitor. Take live readings via resource_manager tool action=status.",
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
