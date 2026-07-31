package friday

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/friday-prototype/friday-go/pkg/db"
)

// SemanticRecallTool gives the AI conscious access to its vector memory.
// Unlike implicit injection (automatic, invisible), this lets the AI
// explicitly search semantically similar facts — deciding when, what,
// and how many results to retrieve.
type SemanticRecallTool struct{}

func (t *SemanticRecallTool) Name() string { return "semantic_recall" }

func (t *SemanticRecallTool) Description() string {
	return "Search verified memory facts by semantic similarity. Use this when you need to recall what the user said or decided about a topic (trading rules, preferences, past analysis, account details). Results include similarity scores. Example: semantic_recall(query='eur usd swing trading rules', limit=5)"
}

func (t *SemanticRecallTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"query": {
				Type:        "string",
				Description: "The concept or question to search memory for (natural language, not keywords)",
			},
			"limit": {
				Type:        "integer",
				Description: "Max results to return (1-20, default 5)",
			},
		},
		Required: []string{"query"},
	}
}

func (t *SemanticRecallTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid args: %w", err)
	}
	if params.Query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if params.Limit <= 0 || params.Limit > 20 {
		params.Limit = 5
	}

	results, err := SemanticRecall(ctx, params.Query, params.Limit)
	if err != nil {
		return nil, fmt.Errorf("semantic recall failed: %w", err)
	}

	if len(results) == 0 {
		return map[string]any{
			"found":   false,
			"message": "No semantically similar facts found in memory.",
			"results": []map[string]any{},
		}, nil
	}

	return map[string]any{
		"found":   true,
		"count":   len(results),
		"results": results,
	}, nil
}

// FTS5RecallTool searches memory via full-text search (keyword).
// Complementary to semantic_recall — use when you know the exact terms.
type FTS5RecallTool struct{}

func (t *FTS5RecallTool) Name() string { return "fts5_recall" }

func (t *FTS5RecallTool) Description() string {
	return "Search memory facts by keyword (FTS5 full-text search). Use when you know exact terms the user mentioned. Results include relevance. Example: fts5_recall(query='prop firm rules', limit=5)"
}

func (t *FTS5RecallTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"query": {
				Type:        "string",
				Description: "Keywords to search for in memory",
			},
			"limit": {
				Type:        "integer",
				Description: "Max results (1-20, default 5)",
			},
		},
		Required: []string{"query"},
	}
}

func (t *FTS5RecallTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid args: %w", err)
	}
	if params.Query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if params.Limit <= 0 || params.Limit > 20 {
		params.Limit = 5
	}

	rows, err := db.Get().QueryContext(ctx,
		`SELECT id, fact, type, tags, created_at FROM memory_facts
		 WHERE id IN (SELECT rowid FROM memory_fts WHERE memory_fts MATCH ?)
		 ORDER BY created_at DESC LIMIT ?`,
		params.Query, params.Limit)
	if err != nil {
		return nil, fmt.Errorf("fts5 search failed: %w", err)
	}
	defer rows.Close()

	var results []map[string]any
	for rows.Next() {
		var id int64
		var fact, memType, tags string
		var createdAt any
		if err := rows.Scan(&id, &fact, &memType, &tags, &createdAt); err != nil {
			continue
		}
		results = append(results, map[string]any{
			"id":    id,
			"fact":  fact,
			"type":  memType,
			"tags":  tags,
			"since": fmt.Sprintf("%v", createdAt),
		})
	}

	if len(results) == 0 {
		return map[string]any{
			"found":   false,
			"message": "No keyword matches in memory.",
			"results": []map[string]any{},
		}, nil
	}

	return map[string]any{
		"found":   true,
		"count":   len(results),
		"results": results,
	}, nil
}