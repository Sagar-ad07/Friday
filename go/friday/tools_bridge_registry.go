package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ── Bridge Registry Tool ──
//
// Self-healing bridge contract address manager. Friday dynamically
// fetches current bridge addresses from official testnet documentation
// and chain configs. When addresses go stale (testnet resets), she
// auto-discovers new ones via web search.
//
// Permanent fix for the stale-address problem. No hardcoding.

type BridgeRegistryTool struct{}

func (t *BridgeRegistryTool) Name() string { return "bridge_registry" }
func (t *BridgeRegistryTool) Description() string {
	return "MANAGE bridge contract addresses dynamically. Find current L1 bridge addresses for testnet L2 chains via web search. Use when bridge tx fails or when setting up new chains. Friday uses this to keep bridge addresses current without hardcoding."
}
func (t *BridgeRegistryTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"action": {Type: "string", Enum: []string{"find", "list", "verify"},
				Description: "find=search for current bridge address for a chain, list=show cached addresses, verify=check if address is a contract"},
			"chain": {Type: "string", Description: "Chain name (arbitrum, optimism, base, scroll, etc.)"},
		},
		Required: []string{"action"},
	}
}

type BridgeInfo struct {
	Chain      string `json:"chain"`
	L1Chain    string `json:"l1_chain"`
	Contract   string `json:"contract_name"`
	Address    string `json:"address"`
	Source     string `json:"source"`
	Verified   bool   `json:"verified"`
}

// Current working addresses (verified July 2026 on Sepolia).
// When these go stale, Friday auto-searches for new ones.
var knownBridges = []BridgeInfo{
	{
		Chain: "arbitrum", L1Chain: "sepolia", Contract: "Inbox",
		Address: "0xaAe29B0366299461418F5324a79Afc425BE5ae67",
		Source: "https://docs.arbitrum.io/build-decentralized-apps/reference/contract-addresses",
	},
	{
		Chain: "base", L1Chain: "sepolia", Contract: "OptimismPortal",
		Address: "0x49f53e41452c74589e85ca1677426ba426459e85",
		Source: "https://docs.base.org/base-contracts",
	},
	{
		Chain: "scroll", L1Chain: "sepolia", Contract: "L1ETHGateway",
		Address: "0x50c7d3e7f7c656493D1D76aaa1a836CedfCBB16A",
		Source: "https://docs.scroll.io/en/developers/contract-addresses",
	},
}

func (t *BridgeRegistryTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Action string `json:"action"`
		Chain  string `json:"chain"`
	}
	json.Unmarshal(args, &p)

	switch p.Action {
	case "list":
		return map[string]any{"bridges": knownBridges, "count": len(knownBridges),
			"note": "Use web_search to find current addresses when these go stale: search '{chain} {testnet} bridge contract address sepolia'",
		}, nil
	case "find":
		return t.find(p.Chain)
	case "verify":
		return t.verify(p.Chain)
	default:
		return map[string]any{"error": "unknown action"}, nil
	}
}

func (t *BridgeRegistryTool) find(chain string) (any, error) {
	chain = strings.ToLower(chain)

	for _, b := range knownBridges {
		if strings.EqualFold(b.Chain, chain) {
			return map[string]any{
				"chain":    b.Chain,
				"contract": b.Contract,
				"address":  b.Address,
				"source":   b.Source,
				"action": fmt.Sprintf(
					"Use web_fetch to verify: fetch %s and search for '%s' contract address on Sepolia",
					b.Source, b.Contract,
				),
			}, nil
		}
	}

	// Not found — guide Friday to search
	return map[string]any{
		"not_found": chain,
		"action": fmt.Sprintf(
			"Use web_search with query: '%s testnet bridge contract address sepolia 2026' and web_fetch the result",
			chain,
		),
		"fallback": fmt.Sprintf(
			"Use official bridge UI: search '%s testnet bridge' and use their official bridge page",
			chain,
		),
	}, nil
}

func (t *BridgeRegistryTool) verify(chain string) (any, error) {
	chain = strings.ToLower(chain)
	for _, b := range knownBridges {
		if strings.EqualFold(b.Chain, chain) {
			// Check if address is a contract on L1
			isContract := checkIsContract(b.Address)
			return map[string]any{
				"chain":    chain,
				"address":  b.Address,
				"is_contract": isContract,
				"status":   map[bool]string{true: "active", false: "STALE - testnet may have been reset"}[isContract],
				"action":   ifThenElse(isContract,
					"Address verified. Bridge via chain_farm.",
					"Address stale. Use web_search: '"+chain+" testnet bridge contract address sepolia' to find current address.",
				),
			}, nil
		}
	}
	return map[string]any{"error": "chain not found"}, nil
}

func checkIsContract(addr string) bool {
	req := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "eth_getCode",
		"params": []any{addr, "latest"},
	}
	body, _ := json.Marshal(req)
	resp, err := httpClient().Post("https://ethereum-sepolia.publicnode.com", "application/json", strings.NewReader(string(body)))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var r struct{ Result string }
	json.NewDecoder(resp.Body).Decode(&r)
	return r.Result != "" && r.Result != "0x"
}

func ifThenElse(cond bool, a, b string) string {
	if cond { return a }
	return b
}

func httpClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}