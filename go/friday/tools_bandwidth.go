package friday

import (
	"context"
	"encoding/json"
	"fmt"
)

type BandwidthTool struct{}

type BandwidthPlatform struct {
	Name           string   `json:"name"`
	Type           string   `json:"type"`
	DailyEarnings  string   `json:"daily_earnings"`
	SetupGuide     string   `json:"setup_guide"`
	Requirements   []string `json:"requirements"`
	PayoutMethod   string   `json:"payout_method"`
	MinPayout      string   `json:"min_payout"`
}

var bandwidthPlatforms = []BandwidthPlatform{
	{
		Name: "Honeygain", Type: "bandwidth sharing",
		DailyEarnings: "$0.5-2.00/day", SetupGuide: "Install Honeygain app on your computer. Creates a passive income by sharing unused internet bandwidth.",
		Requirements: []string{"Windows/Mac/Linux", "Internet connection", "Install and run 24/7"},
		PayoutMethod: "PayPal, Bitcoin", MinPayout: "$20",
	},
	{
		Name: "EarnApp", Type: "bandwidth sharing",
		DailyEarnings: "$0.3-1.00/day", SetupGuide: "Run EarnApp Docker container or install on your computer. Shares bandwidth for web scraping services.",
		Requirements: []string{"Docker or native install", "Internet connection", "Run 24/7"},
		PayoutMethod: "PayPal", MinPayout: "$2.50",
	},
	{
		Name: "IPRoyal Pawns", Type: "bandwidth sharing",
		DailyEarnings: "$0.1-0.50/day", SetupGuide: "Install IPRoyal Pawns app. Shares your internet connection as residential proxies.",
		Requirements: []string{"Windows/Mac/Linux/Android", "Internet connection", "Run 24/7"},
		PayoutMethod: "PayPal, Bitcoin", MinPayout: "$5",
	},
	{
		Name: "PacketStream", Type: "residential proxy",
		DailyEarnings: "$0.1-0.30/day", SetupGuide: "Install PacketStream client. Get paid for unused bandwidth as residential proxy.",
		Requirements: []string{"Windows/Mac/Linux", "Internet connection", "Run 24/7"},
		PayoutMethod: "PayPal", MinPayout: "$5",
	},
	{
		Name: "Traffmonetizer", Type: "bandwidth sharing",
		DailyEarnings: "$0.1-0.30/day", SetupGuide: "Install Traffmonetizer CLI or app. Monetize unused internet traffic.",
		Requirements: []string{"Windows/Mac/Linux/Docker", "Internet connection", "Run 24/7"},
		PayoutMethod: "PayPal, Bitcoin, Payoneer", MinPayout: "$10",
	},
}

func (t *BandwidthTool) Name() string { return "bandwidth" }

func (t *BandwidthTool) Description() string {
	return "Set up and manage bandwidth sharing apps for passive income. Multiple platforms = more earnings."
}

func (t *BandwidthTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"action": {
				Type:        "string",
				Description: "Action: list, estimate, setup",
				Enum:        []string{"list", "estimate", "setup"},
			},
			"platform": {
				Type:        "string",
				Description: "Platform name to set up",
			},
		},
		Required: []string{"action"},
	}
}

func (t *BandwidthTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Action   string `json:"action"`
		Platform string `json:"platform"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return map[string]any{"error": "invalid args: " + err.Error()}, nil
	}

	switch p.Action {
	case "list":
		return t.listPlatforms(), nil
	case "estimate":
		return t.estimateEarnings(), nil
	case "setup":
		return t.setupPlatform(p.Platform), nil
	default:
		return map[string]any{"error": "unknown action: " + p.Action}, nil
	}
}

func (t *BandwidthTool) listPlatforms() map[string]any {
	platforms := make([]map[string]any, len(bandwidthPlatforms))
	for i, p := range bandwidthPlatforms {
		platforms[i] = map[string]any{
			"name":           p.Name,
			"type":           p.Type,
			"daily_earnings": p.DailyEarnings,
			"requirements":   p.Requirements,
			"payout":         fmt.Sprintf("%s (min %s)", p.PayoutMethod, p.MinPayout),
		}
	}

	return map[string]any{
		"action":           "list",
		"platforms":        platforms,
		"count":            len(platforms),
		"total_potential":  "$1.1-4.1/day combined",
		"monthly_potential": "$33-123/month",
		"note":             "Run ALL of them simultaneously on the same computer. Earnings are passive. Setup takes 15 minutes total.",
		"reality_check":    "These are real platforms that pay. Earnings decrease over time as more users join. Withdraw as soon as you hit minimum payout.",
	}
}

func (t *BandwidthTool) estimateEarnings() map[string]any {
	totalLow := 1.1
	totalHigh := 4.1

	return map[string]any{
		"action":            "estimate",
		"daily":             fmt.Sprintf("$%.1f-%.1f/day", totalLow, totalHigh),
		"weekly":            fmt.Sprintf("$%.1f-%.1f/week", totalLow*7, totalHigh*7),
		"monthly":           fmt.Sprintf("$%.0f-%.0f/month", totalLow*30, totalHigh*30),
		"yearly":            fmt.Sprintf("$%.0f-%.0f/year", totalLow*365, totalHigh*365),
		"platforms":         len(bandwidthPlatforms),
		"time_to_setup_min": "$1.1/day with 1 platform (Honeygain) in 5 min",
		"time_to_setup_max": "$4.1/day with all 5 platforms in 15 min",
		"note":              "Requires computer running 24/7. Earnings are passive after setup.",
	}
}

func (t *BandwidthTool) setupPlatform(name string) map[string]any {
	if name == "" {
		platforms := make([]string, len(bandwidthPlatforms))
		for i, p := range bandwidthPlatforms {
			platforms[i] = fmt.Sprintf("%s (%s)", p.Name, p.DailyEarnings)
		}
		return map[string]any{
			"error":     "platform required",
			"available": platforms,
		}
	}

	for _, p := range bandwidthPlatforms {
		if p.Name == name {
			return map[string]any{
				"status":     "setup_guide",
				"platform":   p.Name,
				"guide":      p.SetupGuide,
				"expected":   p.DailyEarnings,
				"payout":     fmt.Sprintf("%s, min %s", p.PayoutMethod, p.MinPayout),
				"next_steps": []string{"Download and install the app", "Create an account", "Let it run 24/7", "Monitor earnings in the dashboard"},
			}
		}
	}

	return map[string]any{"error": fmt.Sprintf("unknown platform: %s", name)}
}
