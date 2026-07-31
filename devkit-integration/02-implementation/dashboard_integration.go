package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// DevKitClient is the interface for interacting with DevKit
type DevKitClient interface {
	CheckBeforeChange(description, changeType string) (*CheckResult, error)
	CreateCheckpoint(description string) (*CheckpointResult, error)
	RollbackToCheckpoint(checkpointID string) (*RollbackResult, error)
	GetJournal(limit int) ([]JournalEntry, error)
	GetCheckpointList() ([]CheckpointInfo, error)
	GetAvailableSkills() ([]DevKitSkill, error)
	LogJournalEntry(entryType string, entryData map[string]interface{}) (*JournalEntry, error)
}

// FridayHookManager manages Friday hooks
type FridayHookManager interface {
	ExecuteBeforeChange(ctx context.Context, changeType string, changeDetails interface{}) error
	ExecuteAfterChange(ctx context.Context, changeType string, changeDetails interface{}, success bool, err error) error
}

// Type aliases for devkit_client types
type CheckResult struct {
	Success     bool   `json:"success"`
	IsApproved  bool   `json:"is_approved"`
	ChangeID    string `json:"change_id,omitempty"`
	Message     string `json:"message"`
	Errors      []string `json:"errors,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
}

type RollbackResult struct {
	Success     bool   `json:"success"`
	RolledBack  bool   `json:"rolled_back"`
	Message     string `json:"message"`
	CheckpointID string `json:"checkpoint_id,omitempty"`
}

type CheckpointResult struct {
	Success    bool   `json:"success"`
	Checkpoint *CheckpointInfo `json:"checkpoint,omitempty"`
	Message    string `json:"message"`
}

type CheckpointInfo struct {
	ID          string    `json:"id"`
	Description string    `json:"description"`
	Timestamp   int64     `json:"timestamp"`
	CreatedBy   string    `json:"created_by"`
}

type JournalEntry struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Data        map[string]interface{} `json:"data"`
	Timestamp   int64                  `json:"timestamp"`
	CreatedBy   string                 `json:"created_by"`
	Message     string                 `json:"message,omitempty"`
}

type DevKitSkill struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

type HealthCheckResult struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	Version   string `json:"version,omitempty"`
	Uptime    int64  `json:"uptime,omitempty"`
}

// Dashboard represents the Friday DevKit Dashboard
type Dashboard struct {
	devKitClient *DevKitClient
	hookManager  *FridayHookManager
	server       *http.Server
	channels     map[string]chan interface{}
	mu           sync.RWMutex
}

// NewDashboard creates a new Dashboard instance
func NewDashboard(devKitClient DevKitClient, hookManager FridayHookManager, port int) *Dashboard {
	return &Dashboard{
		devKitClient: &devKitClient,
		hookManager:  &hookManager,
		server: &http.Server{
			Addr:         fmt.Sprintf(":%d", port),
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
			IdleTimeout:  120 * time.Second,
		},
		channels: make(map[string]chan interface{}),
	}
}

// Start starts the dashboard server
func (d *Dashboard) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", d.handleHealth)
	mux.HandleFunc("/api/check-before-change", d.handleCheckBeforeChange)
	mux.HandleFunc("/api/create-checkpoint", d.handleCreateCheckpoint)
	mux.HandleFunc("/api/rollback-to-checkpoint", d.handleRollback)
	mux.HandleFunc("/api/journal", d.handleJournal)
	mux.HandleFunc("/api/checkpoints", d.handleCheckpoints)
	mux.HandleFunc("/api/skills", d.handleSkills)
	mux.HandleFunc("/api/log-journal", d.handleLogJournal)
	mux.HandleFunc("/api/channels", d.handleChannels)
	mux.HandleFunc("/api/channels/", d.handleChannel)

	d.server.Handler = mux

	fmt.Printf("Starting Friday DevKit Dashboard on http://localhost:%d\n", d.server.Addr[1:])

	return d.server.ListenAndServe()
}

// Stop stops the dashboard server
func (d *Dashboard) Stop() error {
	ctx := context.Background()
	return d.server.Shutdown(ctx)
}

// Broadcast sends data to all connected clients via channels
func (d *Dashboard) Broadcast(channel string, data interface{}) error {
	d.mu.RLock()
	defer d.mu.RUnlock()

	ch, exists := d.channels[channel]
	if !exists {
		return fmt.Errorf("channel not found: %s", channel)
	}

	select {
	case ch <- data:
		return nil
	default:
		return fmt.Errorf("channel is full")
	}
}

// AddChannel adds a new channel for real-time updates
func (d *Dashboard) AddChannel(channel string) chan interface{} {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, exists := d.channels[channel]; exists {
		return nil
	}

	ch := make(chan interface{}, 100)
	d.channels[channel] = ch

	return ch
}

// RemoveChannel removes a channel
func (d *Dashboard) RemoveChannel(channel string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, exists := d.channels[channel]; exists {
		close(d.channels[channel])
		delete(d.channels, channel)
	}
}

// API Handlers

func (d *Dashboard) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	resp := map[string]interface{}{
		"status": "running",
		"port":   d.server.Addr[1:],
		"time":   time.Now().Unix(),
	}

	json.NewEncoder(w).Encode(resp)
}

func (d *Dashboard) handleCheckBeforeChange(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Description  string `json:"description"`
		ChangeType   string `json:"change_type"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	result, err := d.devKitClient.CheckBeforeChange(req.Description, req.ChangeType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)
}

func (d *Dashboard) handleCreateCheckpoint(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Description string `json:"description"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	result, err := d.devKitClient.CreateCheckpoint(req.Description)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)
}

func (d *Dashboard) handleRollback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		CheckpointID string `json:"checkpoint_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	result, err := d.devKitClient.RollbackToCheckpoint(req.CheckpointID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)
}

func (d *Dashboard) handleJournal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	limit := 100
	if r.URL.Query().Has("limit") {
		fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
	}

	entries, err := d.devKitClient.GetJournal(limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(entries)
}

func (d *Dashboard) handleCheckpoints(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	checkpoints, err := d.devKitClient.GetCheckpointList()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(checkpoints)
}

func (d *Dashboard) handleSkills(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	skills, err := d.devKitClient.GetAvailableSkills()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(skills)
}

func (d *Dashboard) handleLogJournal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		EntryType  string                 `json:"type"`
		EntryData  map[string]interface{} `json:"data"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	entry, err := d.devKitClient.LogJournalEntry(req.EntryType, req.EntryData)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(entry)
}

func (d *Dashboard) handleChannels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodPost {
		var req struct {
			Channel string `json:"channel"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		ch := d.AddChannel(req.Channel)
		if ch == nil {
			http.Error(w, "Channel already exists", http.StatusConflict)
			return
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"channel": req.Channel, "status": "created"})
		return
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	channelNames := make([]string, 0, len(d.channels))
	for name := range d.channels {
		channelNames = append(channelNames, name)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(channelNames)
}

func (d *Dashboard) handleChannel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract channel name from URL
	parts := r.URL.Path[len("/api/channels/"):]
	if parts == "" {
		http.Error(w, "Channel name required", http.StatusBadRequest)
		return
	}

	if r.Method == http.MethodDelete {
		d.RemoveChannel(parts)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	ch := d.AddChannel(parts)
	if ch == nil {
		http.Error(w, "Channel already exists", http.StatusConflict)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"channel": parts, "status": "connected"})
}

// Real-time updates
type UpdateMessage struct {
	Type      string      `json:"type"`
	Channel   string      `json:"channel"`
	Data      interface{} `json:"data"`
	Timestamp int64       `json:"timestamp"`
}

// NotifyChange notifies all connected clients of a change
func (d *Dashboard) NotifyChange(changeType string, data interface{}) error {
	return d.Broadcast("changes", UpdateMessage{
		Type:      changeType,
		Channel:   "changes",
		Data:      data,
		Timestamp: time.Now().Unix(),
	})
}

// NotifyCheckpoint notifies of a checkpoint creation
func (d *Dashboard) NotifyCheckpoint(checkpoint *CheckpointInfo) error {
	return d.Broadcast("checkpoints", UpdateMessage{
		Type:      "checkpoint_created",
		Channel:   "checkpoints",
		Data:      checkpoint,
		Timestamp: time.Now().Unix(),
	})
}

// NotifyJournalEntry notifies of a journal entry
func (d *Dashboard) NotifyJournalEntry(entry *JournalEntry) error {
	return d.Broadcast("journal", UpdateMessage{
		Type:      "journal_entry",
		Channel:   "journal",
		Data:      entry,
		Timestamp: time.Now().Unix(),
	})
}

// WebSocket support for real-time updates
// This would require a separate websocket handler package

// Dashboard initialization helpers

func NewDashboardWithDefaults() *Dashboard {
	// In real implementation, this would initialize DevKit client and hooks
	// For now, return a placeholder
	return &Dashboard{}
}

func CreateDashboard(devKitPort, authToken string) *Dashboard {
	// Initialize DevKit client
	client := &DevKitClient{}

	// Initialize hook manager
	hookManager := &FridayHookManager{}

	// Create dashboard with default port 3001
	return NewDashboard(client, hookManager, 3001)
}
