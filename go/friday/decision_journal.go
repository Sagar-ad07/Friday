package friday

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/friday-prototype/friday-go/pkg/db"
)

// ──────────────────────────────────────────────────────────────────────
// Decision Journal — logs every AI decision for learning and debugging
// ──────────────────────────────────────────────────────────────────────

// DecisionEntry records a single AI decision cycle
type DecisionEntry struct {
	ID         int64                  `json:"id"`
	Input      string                 `json:"input"`
	ToolCalls  []ToolCallRecord       `json:"tool_calls,omitempty"`
	Result     string                 `json:"result"`
	Outcome    string                 `json:"outcome"`
	Confidence float64                `json:"confidence"`
	CreatedAt  time.Time              `json:"created_at"`
}

// ToolCallRecord records a single tool invocation within a decision
type ToolCallRecord struct {
	Tool    string `json:"tool"`
	Args    string `json:"args"`
	Result  string `json:"result"`
	Error   string `json:"error,omitempty"`
	DurMs   int64  `json:"dur_ms"`
}

// LogDecision records an AI decision to the journal
func LogDecision(input string, toolCalls []ToolCallRecord, result, outcome string, confidence float64) {
	toolCallsJSON, _ := json.Marshal(toolCalls)
	_, err := db.Get().Exec(
		"INSERT INTO decisions (input, tool_calls, result, outcome, confidence) VALUES (?, ?, ?, ?, ?)",
		input, string(toolCallsJSON), result, outcome, confidence,
	)
	if err != nil {
		log.Printf("[JOURNAL] failed to log decision: %v", err)
	}
}

// GetDecisions retrieves recent decisions
func GetDecisions(ctx context.Context, limit int) ([]DecisionEntry, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := db.Get().QueryContext(ctx,
		"SELECT id, input, tool_calls, result, outcome, confidence, created_at FROM decisions ORDER BY created_at DESC LIMIT ?", limit)
	if err != nil {
		return nil, fmt.Errorf("query decisions: %w", err)
	}
	defer rows.Close()

	var entries []DecisionEntry
	for rows.Next() {
		var e DecisionEntry
		var toolCallsJSON string
		if err := rows.Scan(&e.ID, &e.Input, &toolCallsJSON, &e.Result, &e.Outcome, &e.Confidence, &e.CreatedAt); err != nil {
			continue
		}
		if toolCallsJSON != "" {
			json.Unmarshal([]byte(toolCallsJSON), &e.ToolCalls)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// GetDecisionsByInput searches decisions by input text
func GetDecisionsByInput(ctx context.Context, query string, limit int) ([]DecisionEntry, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := db.Get().QueryContext(ctx,
		"SELECT id, input, tool_calls, result, outcome, confidence, created_at FROM decisions WHERE input LIKE ? ORDER BY created_at DESC LIMIT ?",
		"%"+query+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("query decisions: %w", err)
	}
	defer rows.Close()

	var entries []DecisionEntry
	for rows.Next() {
		var e DecisionEntry
		var toolCallsJSON string
		if err := rows.Scan(&e.ID, &e.Input, &toolCallsJSON, &e.Result, &e.Outcome, &e.Confidence, &e.CreatedAt); err != nil {
			continue
		}
		if toolCallsJSON != "" {
			json.Unmarshal([]byte(toolCallsJSON), &e.ToolCalls)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

var _ = sql.ErrNoRows // ensure sql import used
