package friday

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/friday-prototype/friday-go/pkg/db"
)

// ──────────────────────────────────────────────────────────────────────
// Memory Tools — 3-tier (Working/Episodic/Semantic) with Verified flag
// SQLite-backed with FTS5 full-text search
// ──────────────────────────────────────────────────────────────────────

type MemoryType string

const (
	MemWorking  MemoryType = "WORKING"
	MemEpisodic MemoryType = "EPISODIC"
	MemSemantic MemoryType = "SEMANTIC"
)

type MemoryReadTool struct{}

func (t *MemoryReadTool) Name() string        { return "recall_facts" }
func (t *MemoryReadTool) Description() string { return "Recall facts from memory using full-text search. Filter by query, type, or verified-only." }
func (t *MemoryReadTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"query":         {Type: "string", Description: "Query to search memory (full-text search)"},
			"limit":         {Type: "number", Description: "Max results", Default: 10},
			"type":          {Type: "string", Enum: []string{"WORKING", "EPISODIC", "SEMANTIC"}, Description: "Filter by memory type"},
			"verified_only": {Type: "boolean", Description: "Only return verified facts", Default: false},
		},
		Required: []string{"query"},
	}
}
func (t *MemoryReadTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Query        string `json:"query"`
		Limit        int    `json:"limit"`
		Type         string `json:"type"`
		VerifiedOnly bool   `json:"verified_only"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %v", err)
	}
	if params.Limit == 0 {
		params.Limit = 10
	}

	d := db.Get()
	var rows *sql.Rows
	var err error

	if params.Query != "" {
		// FTS5 full-text search with optional filters
		q := "SELECT f.id, f.fact, f.type, f.verified, f.tags, f.created_at FROM memory_facts f JOIN memory_fts ON f.id = memory_fts.rowid WHERE memory_fts MATCH ?"
		args := []interface{}{params.Query}
		if params.Type != "" {
			q += " AND f.type = ?"
			args = append(args, params.Type)
		}
		if params.VerifiedOnly {
			q += " AND f.verified = 1"
		}
		q += " ORDER BY f.created_at DESC LIMIT ?"
		args = append(args, params.Limit)
		rows, err = d.QueryContext(ctx, q, args...)
	} else {
		// No query — return recent facts with filters
		q := "SELECT id, fact, type, verified, tags, created_at FROM memory_facts WHERE 1=1"
		args := []interface{}{}
		if params.Type != "" {
			q += " AND type = ?"
			args = append(args, params.Type)
		}
		if params.VerifiedOnly {
			q += " AND verified = 1"
		}
		q += " ORDER BY created_at DESC LIMIT ?"
		args = append(args, params.Limit)
		rows, err = d.QueryContext(ctx, q, args...)
	}

	if err != nil {
		return nil, fmt.Errorf("memory query failed: %w", err)
	}
	defer rows.Close()

	facts := []map[string]any{}
	for rows.Next() {
		var id int64
		var fact, memType, tags string
		var verified int
		var createdAt time.Time
		if err := rows.Scan(&id, &fact, &memType, &verified, &tags, &createdAt); err != nil {
			continue
		}
		facts = append(facts, map[string]any{
			"id":         id,
			"fact":       fact,
			"type":       memType,
			"verified":   verified == 1,
			"tags":       tags,
			"created_at": createdAt.Format(time.RFC3339),
		})
	}

	var total int
	d.QueryRowContext(ctx, "SELECT COUNT(*) FROM memory_facts").Scan(&total)

	return map[string]any{"facts": facts, "total": total, "matched": len(facts)}, nil
}

type MemoryWriteTool struct{}

func (t *MemoryWriteTool) Name() string        { return "remember_fact" }
func (t *MemoryWriteTool) Description() string { return "Store a fact in memory with type classification and verification flag" }
func (t *MemoryWriteTool) Schema() ToolSchema {
	return ToolSchema{Type: "object", Properties: map[string]PropertyDef{
		"fact":     {Type: "string", Description: "Fact to remember"},
		"tags":     {Type: "string", Description: "Comma-separated tags"},
		"type":     {Type: "string", Enum: []string{"WORKING", "EPISODIC", "SEMANTIC"}, Description: "Memory type (default: SEMANTIC)"},
		"verified": {Type: "boolean", Description: "Whether this fact is verified/confirmed", Default: false},
	}, Required: []string{"fact"}}
}
func (t *MemoryWriteTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Fact     string `json:"fact"`
		Tags     string `json:"tags"`
		Type     string `json:"type"`
		Verified bool   `json:"verified"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %v", err)
	}
	if params.Fact == "" {
		return nil, fmt.Errorf("fact is required")
	}
	if params.Type == "" {
		params.Type = string(MemSemantic)
	}

	verifiedInt := 0
	if params.Verified {
		verifiedInt = 1
	}

	result, err := db.Get().ExecContext(ctx,
		"INSERT INTO memory_facts (fact, type, verified, tags) VALUES (?, ?, ?, ?)",
		params.Fact, params.Type, verifiedInt, params.Tags)
	if err != nil {
		return nil, fmt.Errorf("failed to store fact: %w", err)
	}

	id, _ := result.LastInsertId()
	var total int
	db.Get().QueryRowContext(ctx, "SELECT COUNT(*) FROM memory_facts").Scan(&total)

	return map[string]any{"success": true, "id": id, "total_facts": total, "type": params.Type, "verified": params.Verified}, nil
}

// ──────────────────────────────────────────────────────────────────────
// Legacy in-memory store (used as fallback if DB not initialized)
// ──────────────────────────────────────────────────────────────────────

var (
	memOnce  sync.Once
	memStore []memoryFact
	memFile  string
	memMu    sync.RWMutex
)

type memoryFact struct {
	Fact      string     `json:"fact"`
	CreatedAt time.Time  `json:"created_at"`
	Tags      []string   `json:"tags"`
	Type      MemoryType `json:"type"`
	Verified  bool       `json:"verified"`
}

func initMemory() {
	memFile = filepath.Join(ProjectRoot, "data", "memory.json")
	os.MkdirAll(filepath.Dir(memFile), 0755)
	data, err := os.ReadFile(memFile)
	if err != nil {
		memStore = []memoryFact{}
		return
	}
	json.Unmarshal(data, &memStore)
	if memStore == nil {
		memStore = []memoryFact{}
	}
}

func saveMemory() {
	data, _ := json.MarshalIndent(memStore, "", "  ")
	os.WriteFile(memFile, data, 0644)
}
