package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// GenerateStatementTool produces a prop firm–ready trade statement.
type GenerateStatementTool struct{}

func (t *GenerateStatementTool) Name() string { return "generate_statement" }
func (t *GenerateStatementTool) Description() string { return "Generate a prop firm trade statement. Period defaults to current week if not specified." }
func (t *GenerateStatementTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"from":      {Type: "string", Description: "Start date (YYYY-MM-DD). Default: 7 days ago"},
			"to":        {Type: "string", Description: "End date (YYYY-MM-DD). Default: today"},
		},
	}
}
func (t *GenerateStatementTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	json.Unmarshal(args, &params)

	from := time.Now().AddDate(0, 0, -7).Truncate(24 * time.Hour)
	to := time.Now().Truncate(24 * time.Hour).Add(24 * time.Hour)
	if params.From != "" {
		if p, err := time.Parse("2006-01-02", params.From); err == nil {
			from = p
		}
	}
	if params.To != "" {
		if p, err := time.Parse("2006-01-02", params.To); err == nil {
			to = p.Add(24 * time.Hour)
		}
	}

	stmt, err := GenerateStatement(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("statement failed: %w", err)
	}
	return map[string]any{"statement": stmt, "from": from.Format("2006-01-02"), "to": to.Format("2006-01-02")}, nil
}

// TradeLedgerTool returns recent trades from the ledger.
type TradeLedgerTool struct{}

func (t *TradeLedgerTool) Name() string { return "trade_ledger" }
func (t *TradeLedgerTool) Description() string { return "Show recent completed trades from the ledger. Optionally set limit (1-100, default 25)." }
func (t *TradeLedgerTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"limit": {Type: "integer", Description: "Number of recent trades to show (1-100)"},
		},
	}
}
func (t *TradeLedgerTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct{ Limit int `json:"limit"` }
	json.Unmarshal(args, &params)

	trades, err := GetTradeLedger(ctx, params.Limit)
	if err != nil {
		return nil, fmt.Errorf("ledger query: %w", err)
	}
	if len(trades) == 0 {
		return map[string]any{"trades": []TradeRecord{}, "count": 0, "message": "No trades in ledger yet."}, nil
	}
	return map[string]any{"trades": trades, "count": len(trades)}, nil
}

// ApproveStrategyTool lets Boss approve a strategy the lab found.
type ApproveStrategyTool struct{}

func (t *ApproveStrategyTool) Name() string { return "approve_strategy" }
func (t *ApproveStrategyTool) Description() string {
	return "Approve a strategy that the Strategy Lab found and is pending approval. Use when Boss says 'approve', 'ok', 'confirm', or 'deploy it'."
}
func (t *ApproveStrategyTool) Schema() ToolSchema {
	return ToolSchema{Type: "object", Properties: map[string]PropertyDef{}}
}
func (t *ApproveStrategyTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	pending := PendingStrategy()
	if pending == nil {
		return map[string]any{"status": "no_pending", "message": "No strategy is pending approval. The lab will alert you when it finds a better one."}, nil
	}
	if err := ApproveStrategy(); err != nil {
		return nil, fmt.Errorf("approve failed: %w", err)
	}
	return map[string]any{
		"status":   "approved",
		"strategy": pending.Name,
		"message":  fmt.Sprintf("%s approved and deployed. Takes effect next trading day.", pending.Name),
	}, nil
}