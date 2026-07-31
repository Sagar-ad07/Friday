package friday

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AccountConfig stores one trading account's credentials and metadata
type AccountConfig struct {
	Name       string  `json:"name"`
	Login      int     `json:"login"`
	Server     string  `json:"server"`
	Password   string  `json:"password"`
	Balance    float64 `json:"balance"`
	Currency   string  `json:"currency"`
	Type       string  `json:"type"` // "propfirm" or "private"
	Leverage   int     `json:"leverage"`
	Active     bool    `json:"active"`
	AddedAt    string  `json:"added_at"`
	LastUsed   string  `json:"last_used,omitempty"`
	DailyLoss  float64 `json:"daily_loss_limit,omitempty"`
	MaxDrawdown float64 `json:"max_drawdown_pct,omitempty"`
	Notes      string  `json:"notes,omitempty"`
}

// AccountManager stores and manages multiple trading accounts
type AccountManager struct {
	mu       sync.RWMutex
	Accounts []AccountConfig `json:"accounts"`
	ActiveID string          `json:"active_id"` // name of currently connected account
	path     string
}

var globalAccounts *AccountManager
var accountsOnce sync.Once

func GetAccounts() *AccountManager {
	accountsOnce.Do(func() {
		path := filepath.Join(ProjectRoot, "data", "accounts.json")
		am := &AccountManager{path: path}
		if data, err := os.ReadFile(path); err == nil {
			if json.Unmarshal(data, am) == nil {
				log.Printf("Accounts loaded: %d accounts, active=%s", len(am.Accounts), am.ActiveID)
			}
		}
		if len(am.Accounts) == 0 {
			// Seed with known accounts
			am.Accounts = append(am.Accounts, AccountConfig{
				Name: "Blue Guardian 5k", Login: 503985, Server: "BlueGuardian-Server",
				Balance: 5000, Currency: "USD", Type: "propfirm", Leverage: 30,
				Active: true, AddedAt: time.Now().Format(time.RFC3339),
				DailyLoss: 150, MaxDrawdown: 5,
				Notes: "Blue Guardian $5k Instant Starter prop firm challenge",
			})
			am.Accounts = append(am.Accounts, AccountConfig{
				Name: "Exness Private", Login: 167036042, Server: "Exness-MT5Real3",
				Balance: 30, Currency: "AED", Type: "private", Leverage: 500,
				Active: true, AddedAt: time.Now().Format(time.RFC3339),
				Notes: "Personal Exness account - no prop firm rules",
			})
			am.ActiveID = "Blue Guardian 5k"
			am.save()
		}
		globalAccounts = am
	})
	return globalAccounts
}

func (am *AccountManager) save() {
	os.MkdirAll(filepath.Dir(am.path), 0755)
	data, _ := json.MarshalIndent(am, "", "  ")
	os.WriteFile(am.path, data, 0644)
}

// GetActive returns the currently connected account
func (am *AccountManager) GetActive() *AccountConfig {
	am.mu.RLock()
	defer am.mu.RUnlock()
	for _, a := range am.Accounts {
		if a.Name == am.ActiveID { return &a }
	}
	return nil
}

// Get returns an account by name
func (am *AccountManager) Get(name string) *AccountConfig {
	am.mu.RLock()
	defer am.mu.RUnlock()
	for _, a := range am.Accounts {
		if a.Name == name { return &a }
	}
	return nil
}

// SetActive switches which account is currently connected
func (am *AccountManager) SetActive(name string) error {
	am.mu.Lock()
	defer am.mu.Unlock()
	for i, a := range am.Accounts {
		if a.Name == name {
			am.Accounts[i].LastUsed = time.Now().Format(time.RFC3339)
			am.ActiveID = name
			am.save()
			return nil
		}
	}
	return fmt.Errorf("account not found: %s", name)
}

// Add registers a new account
func (am *AccountManager) Add(acc AccountConfig) error {
	am.mu.Lock()
	defer am.mu.Unlock()
	for _, a := range am.Accounts {
		if a.Name == acc.Name { return fmt.Errorf("account %s already exists", acc.Name) }
	}
	acc.AddedAt = time.Now().Format(time.RFC3339)
	am.Accounts = append(am.Accounts, acc)
	am.save()
	return nil
}

// Remove deletes an account
func (am *AccountManager) Remove(name string) error {
	am.mu.Lock()
	defer am.mu.Unlock()
	for i, a := range am.Accounts {
		if a.Name == name {
			am.Accounts = append(am.Accounts[:i], am.Accounts[i+1:]...)
			if am.ActiveID == name { am.ActiveID = "" }
			am.save()
			return nil
		}
	}
	return fmt.Errorf("account not found: %s", name)
}

// List returns all accounts
func (am *AccountManager) List() []AccountConfig {
	am.mu.RLock()
	defer am.mu.RUnlock()
	r := make([]AccountConfig, len(am.Accounts))
	copy(r, am.Accounts)
	return r
}

// Summary returns a human-readable account list
func (am *AccountManager) Summary() string {
	am.mu.RLock()
	defer am.mu.RUnlock()
	s := fmt.Sprintf("=== Accounts (%d total) ===\n", len(am.Accounts))
	for _, a := range am.Accounts {
		mark := " "
		if a.Name == am.ActiveID { mark = ">" }
		s += fmt.Sprintf("%s %s (login %d @ %s) $%.0f %s [%s]\n",
			mark, a.Name, a.Login, a.Server, a.Balance, a.Currency, a.Type)
	}
	return s
}
