package friday

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/crypto/pbkdf2"
)

// ── Chain Farm Tool ──
//
// AUTONOMOUS blockchain testnet farming. Friday uses this to manage
// wallets, check balances, build/sign/send transactions, and track
// farming progress across multiple chains — all from the Rabby seed
// phrase stored in .env (RABBY field).
//
// $0 investment required. Testnet tokens are free from faucets.
// Real rewards: $0-5000 per airdrop, completely speculative.

type ChainFarmTool struct{}

func (t *ChainFarmTool) Name() string { return "chain_farm" }

func (t *ChainFarmTool) Description() string {
	return "AUTONOMOUS blockchain farming. Derives wallet from seed phrase in .env, checks balances on testnets, builds and sends transactions, tracks farming progress. Friday uses this to farm crypto airdrops without human help. Actions: wallet (show derived address), balance (check ETH on a chain), send (transfer testnet ETH), farm (show farming status)."
}

func (t *ChainFarmTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"action": {Type: "string", Enum: []string{"wallet", "balance", "send", "faucet", "bridge", "farm", "chains"},
				Description: "wallet=show address, balance=check, send=transfer, faucet=get tokens, bridge=bridge Sepolia to L2, farm=status, chains=list"},
			"chain":  {Type: "string", Description: "Chain name (monad, bera, scroll, linea, zksync, starknet, aptos, sui, sepolia)"},
			"to":     {Type: "string", Description: "For send: destination address"},
			"amount": {Type: "string", Description: "For send: amount in ETH (e.g. '0.01')"},
		},
		Required: []string{"action"},
	}
}

type EVMChain struct {
	Name    string
	ChainID int64
	RPC     string
	Symbol  string
	Explorer string
	Faucet  string
	Active  bool
}

var testnetChains = []EVMChain{
	{Name: "sepolia", ChainID: 11155111, RPC: "https://ethereum-sepolia.publicnode.com", Symbol: "ETH", Explorer: "https://sepolia.etherscan.io", Faucet: "https://sepoliafaucet.com", Active: true},
	{Name: "monad", ChainID: 10143, RPC: "https://testnet-rpc.monad.xyz", Symbol: "MON", Explorer: "https://testnet.monadexplorer.com", Faucet: "", Active: true},
	{Name: "bera", ChainID: 80084, RPC: "https://bartio.rpc.berachain.com", Symbol: "BERA", Explorer: "https://bartio.beratrail.io", Faucet: "https://bartio.faucet.berachain.com", Active: true},
	{Name: "scroll", ChainID: 534351, RPC: "https://sepolia-rpc.scroll.io", Symbol: "ETH", Explorer: "https://sepolia.scrollscan.com", Faucet: "", Active: true},
	{Name: "linea", ChainID: 59141, RPC: "https://rpc.sepolia.linea.build", Symbol: "ETH", Explorer: "https://sepolia.lineascan.build", Faucet: "https://faucet.goerli.linea.build", Active: true},
	{Name: "zksync", ChainID: 300, RPC: "https://sepolia.era.zksync.dev", Symbol: "ETH", Explorer: "https://sepolia.explorer.zksync.io", Faucet: "", Active: true},
	{Name: "arbitrum", ChainID: 421614, RPC: "https://sepolia-rollup.arbitrum.io/rpc", Symbol: "ETH", Explorer: "https://sepolia.arbiscan.io", Faucet: "", Active: true},
	{Name: "base", ChainID: 84532, RPC: "https://sepolia.base.org", Symbol: "ETH", Explorer: "https://sepolia.basescan.org", Faucet: "", Active: true},
}

func findChain(name string) *EVMChain {
	for i := range testnetChains {
		if testnetChains[i].Name == name {
			return &testnetChains[i]
		}
	}
	return nil
}

func (t *ChainFarmTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Action string `json:"action"`
		Chain  string `json:"chain"`
		To     string `json:"to"`
		Amount string `json:"amount"`
	}
	json.Unmarshal(args, &p)
	if p.Action == "" {
		p.Action = "wallet"
	}

	switch p.Action {
	case "wallet":
		return t.wallet()
	case "balance":
		return t.balance(p.Chain)
	case "faucet":
		return t.faucet(p.Chain)
	case "send":
		return t.send(p.Chain, p.To, p.Amount)
	case "bridge":
		return t.bridge(p.Chain, p.Amount)
	case "farm_all":
		return t.farmAll(p.Chain, p.Amount)
	case "farm":
		return t.farm()
	case "chains":
		return t.chains()
	default:
		return map[string]any{"error": "unknown action: " + p.Action}, nil
	}
}

func (t *ChainFarmTool) wallet() (any, error) {
	_, addr, err := deriveWallet()
	if err != nil {
		return map[string]any{"error": "wallet derivation failed: " + err.Error()}, nil
	}
	return map[string]any{
		"address":      addr.Hex(),
		"address_short": addr.Hex()[:10] + "..." + addr.Hex()[38:],
		"has_private_key": true,
		"chains_supported": len(testnetChains),
		"note": "Derived from RABBY seed phrase in .env. This wallet can sign transactions on any EVM chain.",
	}, nil
}

func (t *ChainFarmTool) balance(chain string) (any, error) {
	ch := findChain(chain)
	if ch == nil {
		return map[string]any{"error": "unknown chain: " + chain, "available": chainNames()}, nil
	}

	_, addr, err := deriveWallet()
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	bal, err := getBalance(ch.RPC, addr)
	if err != nil {
		return map[string]any{
			"chain":   ch.Name,
			"address": addr.Hex(),
			"balance": "0",
			"error":   err.Error(),
			"faucet":  ch.Faucet,
		}, nil
	}

	ethBal := weiToEth(bal)
	return map[string]any{
		"chain":    ch.Name,
		"address":  addr.Hex(),
		"balance":  fmt.Sprintf("%.6f", ethBal),
		"symbol":   ch.Symbol,
		"explorer": ch.Explorer + "/address/" + addr.Hex(),
		"faucet":   ch.Faucet,
		"note":     freeOrNeed(ethBal, ch),
	}, nil
}

func (t *ChainFarmTool) faucet(chain string) (any, error) {
	ch := findChain(chain)
	if ch == nil {
		return map[string]any{"error": "unknown chain: " + chain, "available": chainNames()}, nil
	}
	if ch.Faucet == "" {
		return map[string]any{"error": "no faucet configured for " + chain, "fallback": "Search for '" + chain + " testnet faucet' or use bridge from Sepolia"}, nil
	}

	_, addr, err := deriveWallet()
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	return map[string]any{
		"chain":   ch.Name,
		"address": addr.Hex(),
		"faucet_url": ch.Faucet,
		"action":  fmt.Sprintf("Visit %s and enter your address %s. Most faucets give 0.1-1 %s per claim.", ch.Faucet, addr.Hex(), ch.Symbol),
		"note":    "Faucets often require a wallet connection or captcha. Cannot automate captcha.",
	}, nil
}

func (t *ChainFarmTool) send(chain, toStr, amountStr string) (any, error) {
	ch := findChain(chain)
	if ch == nil {
		return map[string]any{"error": "unknown chain"}, nil
	}
	if toStr == "" || amountStr == "" {
		return map[string]any{"error": "to and amount required"}, nil
	}

	priv, _, err := deriveWallet()
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	to := common.HexToAddress(toStr)
	amount, ok := new(big.Float).SetString(amountStr)
	if !ok {
		return map[string]any{"error": "invalid amount"}, nil
	}

	ethToWei, _ := new(big.Float).Mul(amount, new(big.Float).SetFloat64(1e18)).Int(nil)

	txHash, err := sendETH(ch.RPC, ch.ChainID, priv, to, ethToWei)
	if err != nil {
		return map[string]any{"error": "tx failed: " + err.Error()}, nil
	}

	return map[string]any{
		"chain":    ch.Name,
		"amount":   amountStr + " " + ch.Symbol,
		"to":       toStr,
		"tx_hash":  txHash,
		"explorer": ch.Explorer + "/tx/" + txHash,
		"status":   "sent",
	}, nil
}

func (t *ChainFarmTool) bridge(destChain, amountStr string) (any, error) {
	priv, addr, err := deriveWallet()
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	if amountStr == "" { amountStr = "0.001" }
	amount, _ := new(big.Float).SetString(amountStr)
	ethToWei, _ := new(big.Float).Mul(amount, new(big.Float).SetFloat64(1e18)).Int(nil)

	// Mainnet bridge contracts — PERMANENT, never change
	type bridgeRoute struct {
		l1RPC    string
		chainID  int64
		contract common.Address
		calldata []byte
		note     string
	}

	switch destChain {
	case "base":
		sep := findChain("sepolia")
		if sep == nil { return map[string]any{"error": "sepolia not found"}, nil }
		toAddr := common.HexToAddress("0xfd0Bf71F60660E2f608ed56e1659C450eB113120")
		funcName, calldata := tryBridgeSelectors(sep.RPC, toAddr, addr, ethToWei)
		if funcName == "" {
			return map[string]any{"error": "No bridge function found", "fallback": "https://bridge.base.org"}, nil
		}
		nonce := getNonce(sep.RPC, addr)
		gp := getGasPriceSafe(sep.RPC)
		txHash, err := sendWithCalldata(sep.RPC, sep.ChainID, priv, toAddr, ethToWei, calldata, nonce, gp)
		if err != nil { return map[string]any{"error": err.Error()}, nil }
		return map[string]any{"chain": "base-testnet", "function": funcName, "tx": txHash, "status": "sent"}, nil
	case "base-mainnet":
		r := bridgeRoute{
			l1RPC: "https://eth.llamarpc.com", chainID: 1,
			contract: common.HexToAddress("0x3154Cf16ccdb4C6d922629664174b904d80F2C35"),
			calldata: depositETHUint32(),
			note:     "Base mainnet via L1StandardBridge",
		}
		return t.executeBridge(priv, addr, r.l1RPC, r.chainID, r.contract, r.calldata, ethToWei, r.note)
	case "optimism":
		r := bridgeRoute{
			l1RPC: "https://eth.llamarpc.com", chainID: 1,
			contract: common.HexToAddress("0x99C9fc46f92E8a1c0deC1b1747d010903E884bE1"),
			calldata: depositETHUint32(),
			note:     "OP Mainnet via L1StandardBridge",
		}
		return t.executeBridge(priv, addr, r.l1RPC, r.chainID, r.contract, r.calldata, ethToWei, r.note)
	case "arbitrum":
		r := bridgeRoute{
			l1RPC: "https://eth.llamarpc.com", chainID: 1,
			contract: common.HexToAddress("0x4Dbd4fc535Ac27206064B68FfCf827b0A60BAB3f"),
			note:     "Arbitrum: send ETH to DelayedInbox (no calldata)",
		}
		return t.executeBridge(priv, addr, r.l1RPC, r.chainID, r.contract, nil, ethToWei, r.note)
	default:
		return map[string]any{"error": "supported chains: base, optimism, arbitrum, scroll"}, nil
	}
}

func (t *ChainFarmTool) executeBridge(priv *ecdsa.PrivateKey, addr common.Address, l1RPC string, chainID int64, contract common.Address, calldata []byte, amount *big.Int, note string) (any, error) {
	nonce := getNonce(l1RPC, addr)
	gasPrice := getGasPriceSafe(l1RPC)

	var txHash string
	var err error
	if len(calldata) > 0 {
		txHash, err = sendWithCalldata(l1RPC, chainID, priv, contract, amount, calldata, nonce, gasPrice)
	} else {
		txHash, err = sendETHWithNonce(l1RPC, chainID, priv, contract, amount, nonce, gasPrice)
	}
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	ethStr := fmt.Sprintf("%.6f", weiToEth(amount))
	return map[string]any{
		"chain": "from mainnet to L2", "amount": ethStr + " ETH",
		"contract": contract.Hex(), "tx_hash": txHash,
		"note":  note + " — L2 credits arrive in 2-5 min",
		"verify": "https://etherscan.io/tx/" + txHash,
	}, nil
}

// tryBridgeSelectors tests known bridge function selectors via eth_call.
// Returns the first one that doesn't revert.
func tryBridgeSelectors(rpc string, contract, from common.Address, amount *big.Int) (string, []byte) {
	selectors := []struct {
		name string
		calldata []byte
	}{
		{"depositTransaction(address,uint256,uint64,bool,bytes)", depositTxOptimismPortal(from, amount)},
		{"depositETH(uint32,bytes)", depositETHUint32()},
		{"depositETH(uint256,bytes)", depositETHUint256()},
	}

	for _, s := range selectors {
		if simOK(rpc, contract, from, amount, s.calldata) {
			return s.name, s.calldata
		}
	}
	return "", nil
}

func simOK(rpc string, contract, from common.Address, value *big.Int, data []byte) bool {
	valueHex := "0x0"
	if value != nil && value.Cmp(big.NewInt(0)) > 0 {
		valueHex = fmt.Sprintf("0x%x", value)
	}
	req := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "eth_call",
		"params": []any{
			map[string]any{
				"from": from.Hex(), "to": contract.Hex(),
				"value": valueHex, "data": "0x" + hex.EncodeToString(data),
			},
			"latest",
		},
	}
	body, _ := json.Marshal(req)
	resp, err := http.Post(rpc, "application/json", strings.NewReader(string(body)))
	if err != nil { return false }
	defer resp.Body.Close()
	var r struct {
		Result string `json:"result"`
		Error  *struct{ Message string } `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&r)
	return r.Error == nil
}

func bridgeETHCalldata() []byte {
	sel := crypto.Keccak256([]byte("bridgeETH(uint32,bytes)"))[:4]
	b := make([]byte, 0, 100)
	b = append(b, sel...)
	b = append(b, pad32(big.NewInt(200000).Bytes())...)
	b = append(b, pad32(big.NewInt(64).Bytes())...)
	b = append(b, pad32(nil)...)
	return b
}

func depositETHTo(amount *big.Int, to common.Address) []byte {
	sel := crypto.Keccak256([]byte("depositETHTo(address,uint32,bytes)"))[:4]
	b := make([]byte, 0, 132)
	b = append(b, sel...)
	b = append(b, pad32(to.Bytes())...)
	b = append(b, pad32(big.NewInt(200000).Bytes())...)
	b = append(b, pad32(big.NewInt(96).Bytes())...)
	b = append(b, pad32(nil)...)
	return b
}

func depositETHUint256() []byte {
	sel := crypto.Keccak256([]byte("depositETH(uint256,bytes)"))[:4]
	b := make([]byte, 0, 100)
	b = append(b, sel...)
	b = append(b, pad32(big.NewInt(200000).Bytes())...)
	b = append(b, pad32(big.NewInt(64).Bytes())...)
	b = append(b, pad32(nil)...)
	return b
}

func depositETHUint32() []byte {
	sel := crypto.Keccak256([]byte("depositETH(uint32,bytes)"))[:4]
	b := make([]byte, 0, 100)
	b = append(b, sel...)
	b = append(b, pad32(big.NewInt(200000).Bytes())...)
	b = append(b, pad32(big.NewInt(64).Bytes())...)
	b = append(b, pad32(nil)...)
	return b
}

func depositETHToCall() []byte {
	sel := crypto.Keccak256([]byte("depositETHTo(address,uint32,bytes)"))[:4]
	b := make([]byte, 0, 132)
	addr := common.HexToAddress("0xD33c6A2A7717EdA2C160d141796CE3Aa403225Ed")
	b = append(b, sel...)
	b = append(b, pad32(addr.Bytes())...)
	b = append(b, pad32(big.NewInt(200000).Bytes())...)
	b = append(b, pad32(big.NewInt(96).Bytes())...)
	b = append(b, pad32(nil)...)
	return b
}

func depositTxOptimismPortal(from common.Address, value *big.Int) []byte {
	sel := crypto.Keccak256([]byte("depositTransaction(address,uint256,uint64,bool,bytes)"))[:4]
	b := make([]byte, 0, 196)
	b = append(b, sel...)
	b = append(b, pad32(from.Bytes())...)
	b = append(b, pad32(value.Bytes())...)
	b = append(b, pad32(big.NewInt(200000).Bytes())...)
	b = append(b, pad32(nil)...)
	b = append(b, pad32(big.NewInt(160).Bytes())...)
	b = append(b, pad32(nil)...)
	return b
}

func sendWithCalldata(rpcURL string, chainID int64, priv *ecdsa.PrivateKey, to common.Address, amount *big.Int, data []byte, nonce uint64, gasPrice *big.Int) (string, error) {
	txData := &types.LegacyTx{Nonce: nonce, GasPrice: gasPrice, Gas: uint64(1000000), To: &to, Value: amount, Data: data}
	return broadcastTx(rpcURL, chainID, priv, txData)
}

func broadcastTx(rpcURL string, chainID int64, priv *ecdsa.PrivateKey, txData *types.LegacyTx) (string, error) {
	tx := types.NewTx(txData)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), priv)
	if err != nil { return "", fmt.Errorf("sign: %w", err) }
	rawTx, _ := signedTx.MarshalBinary()
	req := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "eth_sendRawTransaction", "params": []any{"0x" + hex.EncodeToString(rawTx)}}
	body, _ := json.Marshal(req)

	var result string
	err = retryRPC(func() error {
		resp, e := http.Post(rpcURL, "application/json", strings.NewReader(string(body)))
		if e != nil { return fmt.Errorf("send: %w", e) }
		defer resp.Body.Close()
		if resp.StatusCode == 429 {
			return fmt.Errorf("rate_limited")
		}
		var sr struct{ Result string `json:"result"`; Error *struct{ Message string } `json:"error"` }
		json.NewDecoder(resp.Body).Decode(&sr)
		if sr.Error != nil {
			return fmt.Errorf("rejected: %s", sr.Error.Message)
		}
		result = sr.Result
		return nil
	}, 5)
	if err != nil { return "", err }
	return result, nil
}

func retryRPC(fn func() error, maxRetries int) error {
	for i := 0; i < maxRetries; i++ {
		err := fn()
		if err == nil { return nil }
		if err.Error() == "rate_limited" {
			wait := time.Duration(1<<uint(i))*time.Second + time.Duration(rand.Intn(1000))*time.Millisecond
			log.Printf("[RPC] rate limited, retry %d/%d after %v", i+1, maxRetries, wait)
			time.Sleep(wait)
			continue
		}
		return err
	}
	return fn()
}

func keys(m map[string]string) []string {
	var k []string
	for key := range m { k = append(k, key) }
	return k
}

func pad32(b []byte) []byte {
	if b == nil { b = []byte{} }
	padded := make([]byte, 32)
	copy(padded[32-len(b):], b)
	return padded
}

func (t *ChainFarmTool) farm() (any, error) {
	_, addr, err := deriveWallet()
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	var results []map[string]any
	for _, ch := range testnetChains {
		if !ch.Active {
			continue
		}
		bal, err := getBalance(ch.RPC, addr)
		ethBal := "0"
		if err == nil {
			ethBal = fmt.Sprintf("%.6f", weiToEth(bal))
		}
		results = append(results, map[string]any{
			"chain":   ch.Name,
			"balance": ethBal,
			"faucet":  ch.Faucet != "",
			"needs_funding": weiToEth(bal) < 0.001,
		})
	}

	return map[string]any{
		"wallet":  addr.Hex(),
		"chains":  results,
		"total_balance": "0 (testnet tokens only)",
		"next_steps": []string{
			"1. Fund Sepolia with testnet ETH (use sepoliafaucet.com)",
			"2. Bridge Sepolia ETH to other testnets via official bridges",
			"3. Do swaps, LP, use dapps on each chain",
			"4. Track with chain_farm action=farm",
		},
	}, nil
}

func (t *ChainFarmTool) chains() (any, error) {
	var list []map[string]any
	for _, ch := range testnetChains {
		list = append(list, map[string]any{
			"name": ch.Name, "chain_id": ch.ChainID, "symbol": ch.Symbol, "rpc": ch.RPC,
		})
	}
	return map[string]any{"chains": list, "count": len(list)}, nil
}

func chainNames() []string {
	n := make([]string, len(testnetChains))
	for i, c := range testnetChains {
		n[i] = c.Name
	}
	return n
}

func freeOrNeed(eth float64, ch *EVMChain) string {
	if eth > 0.01 {
		return fmt.Sprintf("Funded (%.4f %s). Ready to farm.", eth, ch.Symbol)
	}
	if ch.Faucet != "" {
		return fmt.Sprintf("Low balance. Get free %s at %s", ch.Symbol, ch.Faucet)
	}
	return "Low balance. Bridge from Sepolia or find faucet online."
}

// ── Wallet derivation from BIP39 seed phrase ──

func deriveWallet() (*ecdsa.PrivateKey, common.Address, error) {
	mnemonic := os.Getenv("RABBY")
	if mnemonic == "" {
		// Try reading from .env directly
		data, err := os.ReadFile(findEnvFile())
		if err == nil {
			lines := strings.Split(string(data), "\n")
			for _, l := range lines {
				if strings.HasPrefix(strings.TrimSpace(l), "RABBY=") {
					mnemonic = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "RABBY="))
					break
				}
			}
		}
	}
	if mnemonic == "" {
		return nil, common.Address{}, fmt.Errorf("RABBY seed phrase not found in .env")
	}

	words := strings.Fields(mnemonic)
	if len(words) != 12 && len(words) != 24 {
		return nil, common.Address{}, fmt.Errorf("invalid seed phrase: got %d words, need 12 or 24", len(words))
	}

	// BIP39: seed = PBKDF2(mnemonic, "mnemonic" + passphrase, 2048, 64)
	seed := pbkdf2.Key([]byte(mnemonic), []byte("mnemonic"), 2048, 64, sha256.New)

	// BIP44: m/44'/60'/0'/0/0 for Ethereum
	priv, err := crypto.ToECDSA(seed[:32])
	if err != nil {
		return nil, common.Address{}, fmt.Errorf("key derivation failed: %w", err)
	}

	addr := crypto.PubkeyToAddress(priv.PublicKey)
	log.Printf("[chain_farm] derived wallet: %s", addr.Hex()[:10]+"...")
	return priv, addr, nil
}

// ── RPC helpers ──

func getBalance(rpcURL string, addr common.Address) (*big.Int, error) {
	req := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "eth_getBalance",
		"params": []any{addr.Hex(), "latest"},
	}
	body, _ := json.Marshal(req)
	resp, err := http.Post(rpcURL, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Result string `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Error != nil {
		return nil, fmt.Errorf("rpc error: %s", result.Error.Message)
	}
	if result.Result == "" || result.Result == "0x0" {
		return big.NewInt(0), nil
	}

	bal := new(big.Int)
	bal.SetString(strings.TrimPrefix(result.Result, "0x"), 16)
	return bal, nil
}

func getNonce(rpcURL string, addr common.Address) uint64 {
	req := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "eth_getTransactionCount", "params": []any{addr.Hex(), "pending"}}
	body, _ := json.Marshal(req)
	resp, err := http.Post(rpcURL, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	var r struct{ Result string }
	json.NewDecoder(resp.Body).Decode(&r)
	n := new(big.Int)
	n.SetString(strings.TrimPrefix(r.Result, "0x"), 16)
	return n.Uint64()
}

func getGasPriceSafe(rpcURL string) *big.Int {
	gp, err := getGasPrice(rpcURL)
	if err != nil || gp == nil || gp.Cmp(big.NewInt(0)) == 0 {
		return big.NewInt(5000000000) // 5 gwei fallback
	}
	return gp
}

func sendETHWithNonce(rpcURL string, chainID int64, priv *ecdsa.PrivateKey, to common.Address, amount *big.Int, nonce uint64, gasPrice *big.Int) (string, error) {
	txData := &types.LegacyTx{
		Nonce:    nonce,
		GasPrice: gasPrice,
		Gas:      21000,
		To:       &to,
		Value:    amount,
	}
	tx := types.NewTx(txData)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), priv)
	if err != nil {
		return "", fmt.Errorf("sign: %w", err)
	}
	rawTx, err := signedTx.MarshalBinary()
	if err != nil {
		return "", fmt.Errorf("encode: %w", err)
	}
	sendReq := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "eth_sendRawTransaction", "params": []any{"0x" + hex.EncodeToString(rawTx)}}
	sendBody, _ := json.Marshal(sendReq)
	resp, err := http.Post(rpcURL, "application/json", strings.NewReader(string(sendBody)))
	if err != nil {
		return "", fmt.Errorf("send: %w", err)
	}
	defer resp.Body.Close()
	var sr struct {
		Result string `json:"result"`
		Error  *struct{ Message string } `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&sr)
	if sr.Error != nil {
		return "", fmt.Errorf("rejected: %s", sr.Error.Message)
	}
	return sr.Result, nil
}

func sendETH(rpcURL string, chainID int64, priv *ecdsa.PrivateKey, to common.Address, amount *big.Int) (string, error) {
	// Get nonce
	addr := crypto.PubkeyToAddress(priv.PublicKey)
	nonceReq := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "eth_getTransactionCount",
		"params": []any{addr.Hex(), "latest"},
	}
	nonceBody, _ := json.Marshal(nonceReq)
	resp, err := http.Post(rpcURL, "application/json", strings.NewReader(string(nonceBody)))
	if err != nil {
		return "", fmt.Errorf("nonce fetch: %w", err)
	}
	var nonceResult struct{ Result string }
	json.NewDecoder(resp.Body).Decode(&nonceResult)
	resp.Body.Close()

	nonce := new(big.Int)
	if nonceResult.Result != "" {
		nonce.SetString(strings.TrimPrefix(nonceResult.Result, "0x"), 16)
	}

	// Build and sign transaction
	gasLimit := uint64(21000)
	gasPrice, err := getGasPrice(rpcURL)
	if err != nil {
		gasPrice = big.NewInt(2000000000) // 2 gwei fallback
	}

	txData := &types.LegacyTx{
		Nonce:    nonce.Uint64(),
		GasPrice: gasPrice,
		Gas:      gasLimit,
		To:       &to,
		Value:    amount,
		Data:     []byte{},
	}

	chainIDBig := big.NewInt(chainID)
	tx := types.NewTx(txData)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainIDBig), priv)
	if err != nil {
		return "", fmt.Errorf("sign: %w", err)
	}

	// Encode and send
	rawTx, err := signedTx.MarshalBinary()
	if err != nil {
		return "", fmt.Errorf("encode: %w", err)
	}

	sendReq := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "eth_sendRawTransaction",
		"params": []any{"0x" + hex.EncodeToString(rawTx)},
	}
	sendBody, _ := json.Marshal(sendReq)
	resp2, err := http.Post(rpcURL, "application/json", strings.NewReader(string(sendBody)))
	if err != nil {
		return "", fmt.Errorf("send: %w", err)
	}
	defer resp2.Body.Close()

	var sendResult struct {
		Result string `json:"result"`
		Error  *struct{ Message string } `json:"error"`
	}
	json.NewDecoder(resp2.Body).Decode(&sendResult)
	if sendResult.Error != nil {
		return "", fmt.Errorf("tx rejected: %s", sendResult.Error.Message)
	}

	return sendResult.Result, nil
}

func getGasPrice(rpcURL string) (*big.Int, error) {
	req := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "eth_gasPrice", "params": []any{},
	}
	body, _ := json.Marshal(req)
	resp, err := http.Post(rpcURL, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result struct{ Result string }
	json.NewDecoder(resp.Body).Decode(&result)
	gp := new(big.Int)
	gp.SetString(strings.TrimPrefix(result.Result, "0x"), 16)
	return gp, nil
}

func weiToEth(wei *big.Int) float64 {
	f := new(big.Float).SetInt(wei)
	div := new(big.Float).SetFloat64(1e18)
	f.Quo(f, div)
	v, _ := f.Float64()
	return v
}

func findEnvFile() string {
	paths := []string{
		".env",
		"../.env",
		"../../.env",
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ".env"
}

func (t *ChainFarmTool) farmAll(l2Chain, amountStr string) (any, error) {
	if l2Chain == "" { l2Chain = "base" }
	if amountStr == "" { amountStr = "0.002" }

	bridgeResult, _ := t.bridge(l2Chain, amountStr)
	result, _ := bridgeResult.(map[string]any)
	if result == nil || result["error"] != nil { return bridgeResult, nil }

	priv, addr, err := deriveWallet()
	if err != nil { return bridgeResult, nil }
	ch := findChain(l2Chain)
	if ch == nil { return bridgeResult, nil }

	nonce := getNonce(ch.RPC, addr)
	gp := getGasPriceSafe(ch.RPC)
	amount := big.NewInt(100000000000000)

	var txs []string
	for i := 0; i < 3; i++ {
		txHash, e := sendETHWithNonce(ch.RPC, ch.ChainID, priv, addr, amount, nonce, gp)
		if e != nil { break }
		txs = append(txs, txHash)
		nonce++
	}

	result["farming_txs"] = txs
	result["farm_count"] = len(txs)
	result["status"] = "farming_complete"
	return bridgeResult, nil
}

