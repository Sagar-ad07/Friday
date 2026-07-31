package friday

import (
	"context"
	"encoding/json"
	"fmt"
)

// ──────────────────────────────────────────────────────────────────────
// Manage Accounts Tool
// ──────────────────────────────────────────────────────────────────────

type ManageAccountsTool struct{}

func (t *ManageAccountsTool) Name() string { return "manage_accounts" }
func (t *ManageAccountsTool) Description() string { return "List, add, or switch trading accounts" }
func (t *ManageAccountsTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"action":    {Type: "string", Description: "list, add, switch, remove", Enum: []string{"list", "add", "switch", "remove"}},
			"name":      {Type: "string", Description: "Account name (required for add/switch/remove)"},
			"login":     {Type: "number", Description: "MT5 login number (for add)"},
			"server":    {Type: "string", Description: "MT5 server (for add)"},
			"balance":   {Type: "number", Description: "Account balance (for add)"},
			"currency":  {Type: "string", Description: "Currency (for add)"},
			"type":      {Type: "string", Description: "Account type: propfirm or private (for add)"},
		},
		Required: []string{"action"},
	}
}
func (t *ManageAccountsTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Action   string  `json:"action"`
		Name     string  `json:"name,omitempty"`
		Login    int     `json:"login,omitempty"`
		Server   string  `json:"server,omitempty"`
		Balance  float64 `json:"balance,omitempty"`
		Currency string  `json:"currency,omitempty"`
		AccType  string  `json:"type,omitempty"`
	}
	if err := json.Unmarshal(args, &params); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }
	if params.Action == "" { params.Action = "list" }

	am := GetAccounts()
	switch params.Action {
	case "list":
		return map[string]any{
			"accounts": am.List(),
			"active":   am.ActiveID,
			"summary":  am.Summary(),
		}, nil
	case "add":
		if params.Name == "" { return map[string]any{"error": "name required"}, nil }
		err := am.Add(AccountConfig{
			Name: params.Name, Login: params.Login, Server: params.Server,
			Balance: params.Balance, Currency: params.Currency, Type: params.AccType,
			Active: true,
		})
		if err != nil { return map[string]any{"error": err.Error()}, nil }
		return map[string]any{"result": fmt.Sprintf("Account '%s' added", params.Name), "accounts": am.List()}, nil
	case "switch":
		if params.Name == "" { return map[string]any{"error": "name required"}, nil }
		err := am.SetActive(params.Name)
		if err != nil { return map[string]any{"error": err.Error()}, nil }
		return map[string]any{"result": fmt.Sprintf("Switched to '%s'. Make sure MT5 terminal is connected to this server.", params.Name), "active": params.Name}, nil
	case "remove":
		if params.Name == "" { return map[string]any{"error": "name required"}, nil }
		err := am.Remove(params.Name)
		if err != nil { return map[string]any{"error": err.Error()}, nil }
		return map[string]any{"result": fmt.Sprintf("Account '%s' removed", params.Name), "accounts": am.List()}, nil
	default:
		return map[string]any{"error": "unknown action"}, nil
	}
}
