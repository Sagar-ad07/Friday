package friday

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

// ──────────────────────────────────────────────────────────────────────
// Simple In-Memory Store for RustDesk Pending Trades
// ──────────────────────────────────────────────────────────────────────

type MemoryStore struct {
	mu    sync.RWMutex
	store map[string]any
}

var memoryStore = &MemoryStore{
	store: make(map[string]any),
}

func (ms *MemoryStore) Set(key string, value any) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.store[key] = value
}

func (ms *MemoryStore) Get(key string) (any, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	if val, ok := ms.store[key]; ok {
		return val, nil
	}
	return nil, fmt.Errorf("key not found")
}

func (ms *MemoryStore) Delete(key string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	delete(ms.store, key)
}

func (ms *MemoryStore) Range(fn func(key string, value any) bool) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	for k, v := range ms.store {
		if !fn(k, v) {
			break
		}
	}
}

// ──────────────────────────────────────────────────────────────────────
// RustDesk Automation Service
// ──────────────────────────────────────────────────────────────────────

type RustDeskAutomationService struct {
	controlURL string
	apiToken   string
}

var rustDeskService = &RustDeskAutomationService{
	controlURL: os.Getenv("RUSTDESK_CONTROL_URL"),
	apiToken:   os.Getenv("RUSTDESK_API_TOKEN"),
}

func (r *RustDeskAutomationService) SendClick(x, y int, data map[string]any) (bool, string, error) {
	if r.controlURL == "" {
		return false, "RustDesk control URL not configured — set RUSTDESK_CONTROL_URL environment variable", nil
	}

	clickRequest := map[string]any{
		"command":     "click",
		"target":      "deepchart_app",
		"position":    map[string]any{"x": x, "y": y},
		"order_details": map[string]any{
			"symbol":     data["symbol"],
			"type":       data["type"],
			"volume":     data["volume"],
			"sl":         data["sl"],
			"tp":         data["tp"],
			"account":    data["account"],
			"timestamp":  time.Now().Unix(),
			"submitted_at": time.Now().Format(time.RFC3339),
		},
	}

	requestBody, _ := json.Marshal(clickRequest)
	client := &http.Client{Timeout: 5 * time.Second}

	req, _ := http.NewRequest("POST", r.controlURL, bytes.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")
	if r.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+r.apiToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, "Failed to send click to RustDesk", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Sprintf("RustDesk returned status %d", resp.StatusCode), nil
	}

	return true, "Click sent to RustDesk — should appear on deepchart app", nil
}
