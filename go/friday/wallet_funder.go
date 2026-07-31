package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ============================================================================
// WALLET FUNDER - Auto faucet claiming for testnet ETH
// ============================================================================

type WalletFunder struct {
	mu             sync.RWMutex
	clients        map[string]*ethclient.Client
	faucetEndpoints map[string]string
	privateKeys    map[string]string // chain -> private key
	stopCh         chan struct{}
	running        bool
	muWallets      sync.RWMutex
	wallets        map[string]*WalletInfo
}

type WalletInfo struct {
	Address       string    `json:"address"`
	PrivateKey    string    `json:"private_key"`
	Chain         string    `json:"chain"`
	Balance       *big.Int  `json:"balance"`
	LastFunded    time.Time `json:"last_funded"`
	FaucetCount   int       `json:"faucet_count"`
}

func NewWalletFunder() *WalletFunder {
	return &WalletFunder{
		clients: make(map[string]*ethclient.Client),
		faucetEndpoints: map[string]string{
			"ethereum":     "https://sepolia-faucet.pk910.de/api/claim",
			"arbitrum":     "https://faucet.arbitrum.io/api/claim",
			"optimism":     "https://faucet.optimism.io/api/claim",
			"base":         "https://faucet.base.org/api/claim",
			"scroll":       "https://faucet.scroll.io/api/claim",
			"linea":        "https://faucet.linea.build/api/claim",
			"zksync":       "https://faucet.zksync.io/api/claim",
			"starknet":     "https://faucet.starknet.io/api/claim",
			"monad":        "https://faucet.monad.xyz/api/claim",
			"berachain":    "https://faucet.berachain.com/api/claim",
			"layerzero":    "https://faucet.layerzero.network/api/claim",
			"arbitrum_sepolia": "https://faucet.arbitrum.io/api/claim",
			"optimism_sepolia": "https://faucet.optimism.io/api/claim",
			"base_sepolia": "https://faucet.base.org/api/claim",
			"scroll_sepolia": "https://faucet.scroll.io/api/claim",
			"linea_sepolia": "https://faucet.linea.build/api/claim",
			"zksync_sepolia": "https://faucet.zksync.io/api/claim",
		},
		privateKeys: make(map[string]string),
		wallets:     make(map[string]*WalletInfo),
		stopCh:      make(chan struct{}),
	}
}

func (w *WalletFunder) Run(ctx context.Context) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()

	// Initialize RPC clients
	w.initClients()

	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		// Initial funding check
		w.fundAllWallets(ctx)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.fundAllWallets(ctx)
			}
		}
	}()
	log.Println("[WALLET-FUNDER] Started - auto faucet claiming every hour")
}

func (w *WalletFunder) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		close(w.stopCh)
		w.running = false
		log.Println("[WALLET-FUNDER] Stopped")
	}
}

func (w *WalletFunder) initClients() {
	rpcs := map[string]string{
		"ethereum":       os.Getenv("ETH_RPC_URL"),
		"arbitrum":       os.Getenv("ARBITRUM_RPC_URL"),
		"optimism":       os.Getenv("OPTIMISM_RPC_URL"),
		"base":           os.Getenv("BASE_RPC_URL"),
		"scroll":         os.Getenv("SCROLL_RPC_URL"),
		"linea":          os.Getenv("LINEA_RPC_URL"),
		"zksync":         os.Getenv("ZKSYNC_RPC_URL"),
		"starknet":       os.Getenv("STARKNET_RPC_URL"),
		"monad":          os.Getenv("MONAD_RPC_URL"),
		"berachain":      os.Getenv("BERACHAIN_RPC_URL"),
		"layerzero":      os.Getenv("LAYERZERO_RPC_URL"),
	}

	for chain, rpc := range rpcs {
		if rpc != "" {
			client, err := ethclient.Dial(rpc)
			if err == nil {
				w.clients[chain] = client
			}
		}
	}
}

// AddWallet adds a wallet to be auto-funded
func (w *WalletFunder) AddWallet(chain, privateKey string) error {
	privateKey = strings.TrimPrefix(privateKey, "0x")
	key, err := crypto.HexToECDSA(privateKey)
	if err != nil {
		return fmt.Errorf("invalid private key: %v", err)
	}

	address := crypto.PubkeyToAddress(key.PublicKey).Hex()
	
	w.muWallets.Lock()
	defer w.muWallets.Unlock()

	keyStr := chain + "_" + address
	w.wallets[keyStr] = &WalletInfo{
		Address:    address,
		PrivateKey: privateKey,
		Chain:      chain,
	}
	
	// Store private key for chain
	w.mu.Lock()
	w.privateKeys[chain] = privateKey
	w.mu.Unlock()

	log.Printf("[WALLET-FUNDER] Added wallet %s for chain %s", address[:10]+"...", chain)
	return nil
}

// fundAllWallets checks balances and claims from faucets
func (w *WalletFunder) fundAllWallets(ctx context.Context) {
	w.muWallets.RLock()
	wallets := make(map[string]*WalletInfo)
	for _, v := range w.wallets {
		wallets[v.Address] = v
	}
	w.muWallets.RUnlock()

	for _, wallet := range wallets {
		go w.fundWallet(ctx, wallet)
	}
}

func (w *WalletFunder) fundWallet(ctx context.Context, wallet *WalletInfo) {
	client, ok := w.clients[wallet.Chain]
	if !ok {
		log.Printf("[WALLET-FUNDER] No RPC client for chain %s", wallet.Chain)
		return
	}

	// Check current balance
	address := common.HexToAddress(wallet.Address)
	balance, err := client.BalanceAt(ctx, address, nil)
	if err != nil {
		log.Printf("[WALLET-FUNDER] Failed to get balance for %s: %v", wallet.Address, err)
		return
	}

	wallet.Balance = balance

	// If balance > 0.01 ETH, skip
	minBalance := big.NewInt(1e16) // 0.01 ETH in wei
	if balance.Cmp(minBalance) >= 0 {
		return
	}

	// Claim from faucet
	faucetURL, ok := w.faucetEndpoints[wallet.Chain]
	if !ok {
		log.Printf("[WALLET-FUNDER] No faucet endpoint for chain %s", wallet.Chain)
		return
	}

	// Try to claim from faucet
	txHash, err := w.claimFromFaucet(wallet, faucetURL)
	if err != nil {
		log.Printf("[WALLET-FUNDER] Failed to claim for %s: %v", wallet.Address, err)
		return
	}

	log.Printf("[WALLET-FUNDER] ✅ Claimed faucet for %s on %s: %s", wallet.Address[:10]+"...", wallet.Chain, txHash)

	w.muWallets.Lock()
	wallet.LastFunded = time.Now()
	wallet.FaucetCount++
	w.muWallets.Unlock()
}

func (w *WalletFunder) claimFromFaucet(wallet *WalletInfo, faucetURL string) (string, error) {
	// Most faucets use POST with JSON
	payload := map[string]string{
		"address": wallet.Address,
	}

	jsonData, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", faucetURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Friday-WalletFunder/1.0")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	if txHash, ok := result["txHash"].(string); ok && txHash != "" {
		return txHash, nil
	}

	if txHash, ok := result["tx_hash"].(string); ok && txHash != "" {
		return txHash, nil
	}

	return "", fmt.Errorf("no tx hash in response: %v", result)
}

// GetWalletBalance returns current balance for a wallet
func (w *WalletFunder) GetWalletBalance(chain, address string) (*big.Int, error) {
	client, ok := w.clients[chain]
	if !ok {
		return nil, fmt.Errorf("no client for chain %s", chain)
	}

	addr := common.HexToAddress(address)
	return client.BalanceAt(context.Background(), addr, nil)
}

// GetAllWallets returns all registered wallets
func (w *WalletFunder) GetAllWallets() []*WalletInfo {
	w.muWallets.RLock()
	defer w.muWallets.RUnlock()

	result := make([]*WalletInfo, 0, len(w.wallets))
	for _, w := range w.wallets {
		result = append(result, w)
	}
	return result
}