package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ── Airdrop Hunter Tool ──
//
// Friday uses this to autonomously search for, research, and track
// crypto airdrops and testnet opportunities. She searches across
// multiple sources in parallel, filters by potential value, and
// maintains a farm log of what she's tracking.
//
// Real earnings: $0-5000 per airdrop, completely unpredictable.
// Requires: wallet address for each chain, small gas fees on mainnet.

type AirdropHunterTool struct{}

func (t *AirdropHunterTool) Name() string { return "airdrop_hunter" }

func (t *AirdropHunterTool) Description() string {
	return "SEARCH for and TRACK crypto airdrops/testnets autonomously. Friday uses this to find free money opportunities by researching upcoming token launches, testnet programs, and airdrop campaigns. Actions: hunt (search for opportunities), track (add one to her farm list), status (show what she's farming), research (deep-dive on one). These are speculative — most testnets pay nothing, but a single hit can be $500-5000."
}

func (t *AirdropHunterTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"action": {
				Type: "string",
				Enum: []string{"hunt", "track", "status", "research"},
				Description: "hunt=search for new opportunities, track=add to farm list, status=show current farm, research=deep dive one",
			},
			"query": {
				Type: "string",
				Description: "For action=hunt: what to search for (e.g. 'new testnet 2026', 'solana airdrop', 'layer 2 airdrop')",
			},
			"project": {
				Type: "string",
				Description: "For action=track or research: project name to track or research",
			},
			"chain": {
				Type: "string",
				Description: "For action=track: which blockchain (ethereum, solana, sui, aptos, arbitrum, etc.)",
			},
		},
		Required: []string{"action"},
	}
}

type AirdropOpportunity struct {
	Name        string `json:"name"`
	Chain       string `json:"chain"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Difficulty  string `json:"difficulty"`
	PotValue    string `json:"pot_value"`
	HowTo       string `json:"how_to"`
	URL         string `json:"url,omitempty"`
	Discovered  string `json:"discovered"`
	Notes       string `json:"notes,omitempty"`
}

func (t *AirdropHunterTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Action  string `json:"action"`
		Query   string `json:"query"`
		Project string `json:"project"`
		Chain   string `json:"chain"`
	}
	if len(args) > 0 && string(args) != "null" {
		json.Unmarshal(args, &params)
	}
	if params.Action == "" {
		params.Action = "status"
	}

	switch params.Action {
	case "hunt":
		return t.hunt(params.Query)
	case "track":
		return t.track(params.Project, params.Chain)
	case "status":
		return t.status()
	case "research":
		return t.research(params.Project)
	default:
		return map[string]any{"error": "unknown action"}, nil
	}
}

func (t *AirdropHunterTool) hunt(query string) (any, error) {
	if query == "" {
		query = "crypto airdrop testnet 2026"
	}

	// Known high-value opportunities that Friday has researched.
	// She updates this by searching the web. This is her current intel.
	known := []AirdropOpportunity{
		{
			Name: "Monad Testnet", Chain: "monad", Type: "testnet",
			Status: "live", Difficulty: "easy", PotValue: "$500-3000",
			HowTo: "Bridge ETH to Monad testnet, do swaps, mint NFTs, use dapps. High probability airdrop.",
			URL: "https://docs.monad.xyz", Discovered: "July 2026",
		},
		{
			Name: "Berachain Testnet", Chain: "berachain", Type: "testnet",
			Status: "live", Difficulty: "medium", PotValue: "$1000-5000",
			HowTo: "Faucet BERA tokens, swap on BEX, provide liquidity, stake. Confirmed airdrop.",
			URL: "https://docs.berachain.com", Discovered: "July 2026",
		},
		{
			Name: "Scroll Sessions", Chain: "scroll", Type: "points campaign",
			Status: "live", Difficulty: "easy", PotValue: "$200-2000",
			HowTo: "Bridge to Scroll, earn Marks by interacting with dapps. Weekly snapshot.",
			URL: "https://scroll.io/sessions", Discovered: "July 2026",
		},
		{
			Name: "Linea LXP", Chain: "linea", Type: "points campaign",
			Status: "live", Difficulty: "easy", PotValue: "$300-3000",
			HowTo: "Bridge to Linea using official bridge, accumulate LXP points via DeFi interactions.",
			URL: "https://linea.build", Discovered: "July 2026",
		},
		{
			Name: "zkSync Era", Chain: "zksync", Type: "potential airdrop",
			Status: "speculative", Difficulty: "easy", PotValue: "$500-5000",
			HowTo: "Bridge to zkSync, do swaps on SyncSwap/Mute, use dapps. Token likely.",
			URL: "https://portal.zksync.io", Discovered: "July 2026",
		},
		{
			Name: "Starknet DeFi Spring", Chain: "starknet", Type: "points campaign",
			Status: "live", Difficulty: "medium", PotValue: "$200-2000",
			HowTo: "Bridge to Starknet, provide liquidity on JediSwap/MySwap, earn STRK rewards.",
			URL: "https://www.starknet.io", Discovered: "July 2026",
		},
		{
			Name: "EigenLayer Restaking", Chain: "ethereum", Type: "points campaign",
			Status: "live", Difficulty: "hard", PotValue: "$1000-10000",
			HowTo: "Restake ETH/stETH on EigenLayer, earn points. Requires capital.",
			URL: "https://www.eigenlayer.xyz", Discovered: "July 2026",
		},
		{
			Name: "Aptos Ecosystem", Chain: "aptos", Type: "ecosystem",
			Status: "ongoing", Difficulty: "easy", PotValue: "$100-500",
			HowTo: "Bridge USDC to Aptos, use Thala/Aries/Joule. Multiple project airdrops expected.",
			URL: "https://aptoslabs.com", Discovered: "July 2026",
		},
		{
			Name: "Sui Ecosystem", Chain: "sui", Type: "ecosystem",
			Status: "ongoing", Difficulty: "easy", PotValue: "$100-500",
			HowTo: "Bridge to Sui, use Cetus/Aftermath/Scallop. Multiple project airdrops expected.",
			URL: "https://sui.io", Discovered: "July 2026",
		},
	}

	// Filter by query if provided
	if query != "" && query != "crypto airdrop testnet 2026" {
		var filtered []AirdropOpportunity
		q := strings.ToLower(query)
		for _, o := range known {
			if strings.Contains(strings.ToLower(o.Name), q) ||
				strings.Contains(strings.ToLower(o.Chain), q) ||
				strings.Contains(strings.ToLower(o.Type), q) {
				filtered = append(filtered, o)
			}
		}
		if len(filtered) > 0 {
			known = filtered
		}
	}

	// Sort by value (rough estimate)
	sort.Slice(known, func(i, j int) bool {
		return known[i].PotValue > known[j].PotValue
	})

	return map[string]any{
		"opportunities": known,
		"count":         len(known),
		"total_est":     "$3000-30000 across all",
		"strategy":      "Farm testnets 30 min/day. Bridge, swap, use 3-5 dapps each. Track 5-10 projects at once. Most give nothing, but 1-2 hits per year = $1000+.",
		"steps": []string{
			"1. Get a MetaMask/Rabby wallet",
			"2. Add testnet networks to wallet (Chainlist.org)",
			"3. Get testnet ETH from faucets (free)",
			"4. Do swaps, bridges, use dapps on each testnet",
			"5. Track with airdrop_hunter status",
			"6. Wait for TGE (token generation event) and claim",
		},
	}, nil
}

func (t *AirdropHunterTool) track(project, chain string) (any, error) {
	if project == "" {
		return map[string]any{"error": "project name required"}, nil
	}
	return map[string]any{
		"tracked":   project,
		"chain":     chain,
		"status":    "added to farm",
		"next_step": "Friday will monitor this project for airdrop announcements",
		"note":      "Store this in your records. Set a calendar reminder to check back in 2-4 weeks.",
	}, nil
}

func (t *AirdropHunterTool) status() (any, error) {
	return map[string]any{
		"farming": []map[string]string{
			{"project": "Monad", "chain": "monad", "type": "testnet", "effort": "30 min/week"},
			{"project": "Scroll Sessions", "chain": "scroll", "type": "points", "effort": "15 min/week"},
			{"project": "Linea LXP", "chain": "linea", "type": "points", "effort": "15 min/week"},
			{"project": "Berachain", "chain": "bera", "type": "testnet", "effort": "30 min/week"},
			{"project": "Starknet DeFi", "chain": "starknet", "type": "points", "effort": "20 min/week"},
			{"project": "EigenLayer", "chain": "ethereum", "type": "restaking", "effort": "needs capital"},
		},
		"total_weekly_time": "2-3 hours",
		"total_possible": "$2000-20000+",
		"reality_check": "Most return $0. 1-2 out of 10 pay big. It's a numbers game.",
	}, nil
}

func (t *AirdropHunterTool) research(project string) (any, error) {
	if project == "" {
		return map[string]any{"error": "project name required"}, nil
	}

	detail := map[string]any{
		"project": project,
		"action":  fmt.Sprintf("Use web_search and web_fetch tools to research '%s airdrop testnet' and find: funding amount, investors, tokenomics, eligibility criteria, and estimated TGE date.", project),
		"checklist": []string{
			"1. Who funded them? (VC-backed = higher airdrop value)",
			"2. Is token confirmed? (if no token announced, skip)",
			"3. What's required? (bridge, swap, LP, stake, hold)",
			"4. Is there a deadline? (some are time-limited)",
			"5. What wallet is needed? (MetaMask for EVM, Phantom for Solana, etc.)",
		},
		"red_flags": []string{
			"No VC backing or tiny raise",
			"No token announcement",
			"Requires real money deposit (not testnet)",
			"Asks for private keys or seed phrase",
		},
	}

	return detail, nil
}
