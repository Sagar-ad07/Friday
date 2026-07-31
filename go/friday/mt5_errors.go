package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// MT5 retcode knowledge table.
// Source: MetaQuotes MT5 TradeRequest retcode constants.
// Used by the orchestrator and as a tool so the agent can explain broker
// errors (e.g. "retcode 10019" -> "no tick available: symbol not in Market
// Watch / market closed / broker not streaming; remediation: SymbolSelect").

type mt5Error struct {
	Code         string
	Constant     string // MetaQuotes symbol name
	Severity     string // ok | info | warn | err
	Explanation  string // short human answer
	Remediation  string // next step Friday should try
}

var mt5Errors = map[int]mt5Error{
	0:     {"0",     "TRADE_RETCODE_DONE", "ok", "Request completed successfully.", ""},
	10008: {"10008", "TRADE_RETCODE_PLACED",            "info", "Order placed.", ""},
	10009: {"10009", "TRADE_RETCODE_DONE",              "ok",   "Request completed.", ""},
	10010: {"10010", "TRADE_RETCODE_DONE_PARTIAL",      "warn", "Only part of the request was executed.", "Check deal/order state via get_positions."},
	10011: {"10011", "TRADE_RETCODE_ERROR",             "err",  "Error processing the request.", "Inspect logs for deeper cause."},
	10012: {"10012", "TRADE_RETCODE_TIMEOUT",           "warn", "No prices for the symbol right now.", "Retry, or warm up via SymbolSelect before next trade."},
	10013: {"10013", "TRADE_RETCODE_INVALID",           "err",  "Invalid request — bad parameters (volume, type, sl/tp).", "Re-check broker min/max volumes and stops distance."},
	10014: {"10014", "TRADE_RETCODE_INVALID_VOLUME",    "err",  "Volume outside broker min/max/step.", "Query SymbolInfo and snap to allowed step."},
	10015: {"10015", "TRADE_RETCODE_INVALID_PRICE",     "err",  "Price invalid — too close to market or out of range.", "Refresh tick (SymbolSelect + SymbolInfoTick) and resend."},
	10016: {"10016", "TRADE_RETCODE_INVALID_STOPS",     "err",  "SL/TP violate broker stops level.", "Move SL/TP further; query SymbolInfo stops_level."},
	10017: {"10017", "TRADE_RETCODE_TRADE_DISABLED",    "err",  "Trading is disabled for this symbol or account.", "Check account permissions; verify terminal 'Algo Trading' is enabled."},
	10018: {"10018", "TRADE_RETCODE_MARKET_CLOSED",    "info", "Market is closed for this symbol.", "Wait for session open; do not retry intraday."},
	10019: {"10019", "TRADE_RETCODE_NO_MONEY_ACCOUNT", "err",  "No price tick available for the symbol.",
		"Most common cause: symbol not in Market Watch OR market closed OR broker not streaming ticks. Fix: call SymbolSelect(symbol, true) before SymbolInfoTick, or wait for market open. On prop firms like BlueGuardian this can also mean tick feed for that suffix (e.g. EURUSDm) is intentionally blocked."},
	10020: {"10020", "TRADE_RETCODE_NO_MONEY",         "err",  "Not enough free margin.", "Reduce volume or fund account."},
	10021: {"10021", "TRADE_RETCODE_PRICE_CHANGED",    "warn", "Price changed since the request.", "Resend at new price."},
	10022: {"10022", "TRADE_RETCODE_PRICE_OFF",        "err",  "No prices for the symbol.", "Same as 10019 — SymbolSelect + wait for market."},
	10023: {"10023", "TRADE_RETCODE_INVALID_EXPIRATION","err", "Invalid order expiration.", "Drop expiration or use GTC."},
	10024: {"10024", "TRADE_RETCODE_INVALID_FILL",     "err",  "Unsupported fill type.", "Switch to IOC/FOK per SymbolInfo filling_mode."},
	10025: {"10025", "TRADE_RETCODE_NO_CONNECTION",    "err",  "No connection to the trade server.", "Check terminal connectivity; reconnect MT5."},
	10026: {"10026", "TRADE_RETCODE_PRICE_CHANGED_REQ","warn", "Price changed since the last request.", "Resend at new price."},
	10027: {"10027", "TRADE_RETCODE_POSITION_ONLY",    "err",  "Only position-close allowed for this symbol.", "Use a close-only request."},
	10028: {"10028", "TRADE_RETCODE_REQUOTE",          "warn", "Broker requoted the price.", "Resend at new price or use market deviation."},
	10029: {"10029", "TRADE_RETCODE_REQUOTE_REJECTED", "err",  "Requote rejected.", "Resend with wider deviation."},
	10030: {"10030", "TRADE_RETCODE_INVALID_FILL_TYPE","err",  "Fill type not supported.", "Retry with IOC."},
	10031: {"10031", "TRADE_RETCODE_INVALID_EXPIRATION_TYPE","err","Unsupported expiration type.", "Drop expiration or use GTC."},
}

// LookupMT5Error returns the structured explanation for a retcode.
func LookupMT5Error(code int) (mt5Error, bool) {
	e, ok := mt5Errors[code]
	return e, ok
}

// LookupMT5ErrorString scans a raw error string for any 5-digit MT5 retcode
// (e.g. "retcode 10019") and returns a consolidated human-readable explanation.
// Empty string if no recognised code is found.
func LookupMT5ErrorString(s string) string {
	var sb strings.Builder
	seen := map[int]bool{}
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool {
		return !(r >= '0' && r <= '9')
	}) {
		if len(tok) < 4 || len(tok) > 5 {
			continue
		}
		if !strings.HasPrefix(tok, "10") {
			continue
		}
		n, err := strconv.Atoi(tok)
		if err != nil {
			continue
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		if e, ok := LookupMT5Error(n); ok {
			fmt.Fprintf(&sb, "MT5 %s (%s): %s", e.Code, e.Constant, e.Explanation)
			if e.Remediation != "" {
				fmt.Fprintf(&sb, " → %s", e.Remediation)
			}
			sb.WriteString(".  ")
		}
	}
	return strings.TrimSpace(sb.String())
}

// ──────────────────────────────────────────────────────────────────────
// explain_mt5_error — a real tool the agent can call to translate raw MT5
// error strings (or retcodes) into actionable explanations.
// ──────────────────────────────────────────────────────────────────────

type ExplainMT5ErrorTool struct{}

func (t *ExplainMT5ErrorTool) Name() string { return "explain_mt5_error" }

func (t *ExplainMT5ErrorTool) Description() string {
	return "Translate an MT5 broker retcode (e.g. 10019) or a raw error string (e.g. 'exec: retcode 10019, no tick available') into plain language with the likely cause and the next remediation step. Call this whenever you see an MT5 retcode in a tool result you don't understand."
}

func (t *ExplainMT5ErrorTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"code":       {Type: "number", Description: "MT5 retcode (e.g. 10019). Mutually exclusive with raw_error."},
			"raw_error":  {Type: "string", Description: "Raw error string to scan for any embedded MT5 retcode (e.g. \"exec: tick: mt5: no tick available\" or \"retcode 10019\")."},
		},
		Required: []string{},
	}
}

func (t *ExplainMT5ErrorTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Code      int    `json:"code"`
		RawError  string `json:"raw_error"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %v", err)
	}

	out := map[string]any{
		"tool":   "explain_mt5_error",
	}

	if p.Code > 0 {
		e, ok := LookupMT5Error(p.Code)
		if !ok {
			out["found"] = false
			out["input_code"] = p.Code
			out["explanation"] = fmt.Sprintf("Retcode %d is not in Friday's local MT5 knowledge table. It may be a vendor-specific code; check the broker docs.", p.Code)
			return out, nil
		}
		out["found"] = true
		out["input_code"] = p.Code
		out["code"] = e.Code
		out["constant"] = e.Constant
		out["severity"] = e.Severity
		out["explanation"] = e.Explanation
		if e.Remediation != "" {
			out["remediation"] = e.Remediation
		}
		return out, nil
	}

	if p.RawError != "" {
		expl := LookupMT5ErrorString(p.RawError)
		if expl == "" {
			out["found"] = false
			out["input_raw"] = p.RawError
			out["explanation"] = "No recognized 5-digit MT5 retcode found in this string. Note 10019 specifically means 'no tick available' — symbol not in Market Watch, market closed, or broker not streaming ticks."
			return out, nil
		}
		out["found"] = true
		out["input_raw"] = p.RawError
		out["explanation"] = expl
		return out, nil
	}

	return out, fmt.Errorf("pass either 'code' or 'raw_error'")
}