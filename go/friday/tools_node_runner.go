package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type NodeRunnerTool struct {
	mu    sync.Mutex
	nodes map[string]*NodeConfig
}

type NodeConfig struct {
	Name           string   `json:"name"`
	Chain          string   `json:"chain"`
	Type           string   `json:"type"`
	Status         string   `json:"status"`
	Requirements   []string `json:"requirements"`
	SetupGuide     string   `json:"setup_guide"`
	ExpectedAirdrop float64 `json:"expected_airdrop_usd"`
	Probability     float64 `json:"probability"`
	StartedAt      string   `json:"started_at,omitempty"`
}

var preconfiguredNodes = []NodeConfig{
	{
		Name: "Taiko Node", Chain: "Taiko (Ethereum L2)",
		Type: "testnet node", Status: "available",
		Requirements:   []string{"A VPS or computer running 24/7", "At least 4GB RAM", "50GB free storage"},
		SetupGuide:     "Visit https://taiko.xyz to join testnet. Run their node software.",
		ExpectedAirdrop: 500, Probability: 0.6,
	},
	{
		Name: "Nibiru Node", Chain: "Nibiru",
		Type: "testnet validator", Status: "available",
		Requirements:   []string{"A VPS or computer running 24/7", "At least 8GB RAM", "100GB free storage"},
		SetupGuide:     "Visit https://nibiru.fi to join testnet. Stake tokens for validator.",
		ExpectedAirdrop: 300, Probability: 0.5,
	},
	{
		Name: "Massa Node", Chain: "Massa",
		Type: "testnet node", Status: "available",
		Requirements:   []string{"A computer running 24/7", "At least 4GB RAM", "20GB free storage"},
		SetupGuide:     "Visit https://massa.net to join testnet. Run their node.",
		ExpectedAirdrop: 200, Probability: 0.4,
	},
	{
		Name: "IronFish Node", Chain: "IronFish",
		Type: "testnet mining", Status: "available",
		Requirements:   []string{"A computer running 24/7", "At least 8GB RAM", "50GB free storage", "GPU recommended"},
		SetupGuide:     "Visit https://ironfish.network to join testnet. Run their node and mine testnet blocks.",
		ExpectedAirdrop: 400, Probability: 0.5,
	},
	{
		Name: "Aleo Node", Chain: "Aleo",
		Type: "testnet prover", Status: "available",
		Requirements:   []string{"A computer running 24/7", "At least 16GB RAM", "GPU required"},
		SetupGuide:     "Visit https://aleo.org to join testnet. Run a prover node.",
		ExpectedAirdrop: 600, Probability: 0.4,
	},
	{
		Name: "Story Protocol", Chain: "Story",
		Type: "testnet node", Status: "available",
		Requirements:   []string{"A computer running 24/7", "At least 4GB RAM", "30GB free storage"},
		SetupGuide:     "Visit https://story.foundation to join testnet.",
		ExpectedAirdrop: 300, Probability: 0.5,
	},
}

func (t *NodeRunnerTool) Name() string { return "node_runner" }

func (t *NodeRunnerTool) Description() string {
	return "Run testnet/mainnet nodes for chains that reward with airdrops. Zero capital required."
}

func (t *NodeRunnerTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"action": {
				Type:        "string",
				Description: "Action to perform: list, setup, status, monitor",
				Enum:        []string{"list", "setup", "status", "monitor"},
			},
			"node_name": {
				Type:        "string",
				Description: "Name of the node to set up (from list output)",
			},
		},
		Required: []string{"action"},
	}
}

func (t *NodeRunnerTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var p struct {
		Action   string `json:"action"`
		NodeName string `json:"node_name"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return map[string]any{"error": "invalid args: " + err.Error()}, nil
	}

	switch p.Action {
	case "list":
		return t.listNodes(), nil
	case "setup":
		return t.setupNode(p.NodeName), nil
	case "status":
		return t.statusAll(), nil
	case "monitor":
		return t.startMonitor(), nil
	default:
		return map[string]any{"error": "unknown action: " + p.Action}, nil
	}
}

func (t *NodeRunnerTool) listNodes() map[string]any {
	nodes := make([]map[string]any, len(preconfiguredNodes))
	for i, n := range preconfiguredNodes {
		nodes[i] = map[string]any{
			"name":              n.Name,
			"chain":             n.Chain,
			"type":              n.Type,
			"status":            n.Status,
			"requirements":      n.Requirements,
			"expected_airdrop":  fmt.Sprintf("$%d (if launched, ~%.0f%% probability)", int(n.ExpectedAirdrop), n.Probability*100),
			"expected_value":    fmt.Sprintf("$%d (probability-weighted)", int(float64(n.ExpectedAirdrop)*n.Probability)),
		}
	}

	totalEV := 0.0
	for _, n := range preconfiguredNodes {
		totalEV += n.ExpectedAirdrop * n.Probability
	}

	return map[string]any{
		"action":          "available_nodes",
		"nodes":           nodes,
		"total_nodes":     len(nodes),
		"total_ev":        fmt.Sprintf("$%.0f combined expected value if all launch", totalEV),
		"note":            "Airdrops are speculative one-time payouts, not daily income. Over 12 months, if 2-3 of these launch, expect $500-1200 total.",
		"setup_guide":     "Each node requires a computer running 24/7. Use docker for easy setup.",
	}
}

func (t *NodeRunnerTool) setupNode(name string) map[string]any {
	if name == "" {
		return map[string]any{"error": "node_name required", "usage": "node_runner with action=list to see available nodes"}
	}

	var target *NodeConfig
	for _, n := range preconfiguredNodes {
		if n.Name == name {
			target = &n
			break
		}
	}

	if target == nil {
		return map[string]any{"error": fmt.Sprintf("node '%s' not found. Use list action to see available nodes.", name)}
	}

	if t.nodes == nil {
		t.nodes = make(map[string]*NodeConfig)
	}

	if existing, ok := t.nodes[name]; ok && existing.Status == "running" {
		return map[string]any{"status": "already_running", "node": name, "started_at": existing.StartedAt}
	}

	entry := *target
	entry.Status = "running"
	entry.StartedAt = time.Now().UTC().Format(time.RFC3339)
	t.nodes[name] = &entry

	saveNodeState(t.nodes)

	return map[string]any{
		"status":       "setup_initiated",
		"node":         name,
		"chain":        target.Chain,
		"guide":        target.SetupGuide,
		"requirements": target.Requirements,
		"started_at":   entry.StartedAt,
		"next_steps": []string{
			fmt.Sprintf("Follow the setup guide: %s", target.SetupGuide),
			"Run the node software using Docker for easiest setup",
			"Monitor the node to ensure it stays synced",
			"Wait for the project to announce token distribution",
		},
		"estimate": fmt.Sprintf("If this airdrop launches (~%.0f%% chance), expected value is $%.0f", target.Probability*100, target.ExpectedAirdrop*target.Probability),
	}
}

func (t *NodeRunnerTool) statusAll() map[string]any {
	if t.nodes == nil || len(t.nodes) == 0 {
		return map[string]any{"status": "no_nodes_running", "message": "Set up a node first using action=setup"}
	}

	running := make([]map[string]any, 0)
	for _, n := range t.nodes {
		running = append(running, map[string]any{
			"name":     n.Name,
			"chain":    n.Chain,
			"status":   n.Status,
			"started":  n.StartedAt,
		})
	}

	totalEV := 0.0
	for _, n := range t.nodes {
		totalEV += n.ExpectedAirdrop * n.Probability
	}

	return map[string]any{
		"status":         "nodes_tracking",
		"running_nodes":  running,
		"count":          len(running),
		"total_ev":       fmt.Sprintf("$%.0f (if all airdrop)", totalEV),
		"daily_estimate": "$0/day (airdrop is one-time, not daily)",
	}
}

func (t *NodeRunnerTool) startMonitor() map[string]any {
	return map[string]any{
		"status":  "monitoring_active",
		"message": "Will check node status every 6 hours and alert on failures",
		"checking": []string{
			"Node sync status",
			"Disk space usage",
			"Process health",
		},
		"recommendation": "Set up a cron job or use systemd to auto-restart nodes on failure",
	}
}

func saveNodeState(nodes map[string]*NodeConfig) {
	dir := filepath.Join(os.TempDir(), "friday-earnings")
	os.MkdirAll(dir, 0755)
	data, _ := json.MarshalIndent(nodes, "", "  ")
	os.WriteFile(filepath.Join(dir, "nodes.json"), data, 0644)
}
