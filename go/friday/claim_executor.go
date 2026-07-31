package friday

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ============================================================================
// CLAIM EXECUTOR - Auto-claims rewards at mainnet launch
// ============================================================================

type ClaimExecutor struct {
	mu           sync.RWMutex
	claims       map[string]*ClaimJob
	clients      map[string]*ethclient.Client
	walletFunder *WalletFunder
	stopCh       chan struct{}
	running      bool
}

type ClaimJob struct {
	ID            string                 `json:"id"`
	CampaignID    string                 `json:"campaign_id"`
	Chain         string                 `json:"chain"`
	WalletAddress string                 `json:"wallet_address"`
	ClaimContract string                 `json:"claim_contract"`
	ClaimABI      string                 `json:"claim_abi"`
	ClaimMethod   string                 `json:"claim_method"` // "claim", "claimRewards", "claimAll"
	MethodParams  []interface{}          `json:"method_params"`
	Status        string                 `json:"status"` // pending, scheduled, executing, completed, failed
	ScheduledAt   time.Time              `json:"scheduled_at"`
	ExecutedAt    *time.Time             `json:"executed_at,omitempty"`
	TxHash        string                 `json:"tx_hash,omitempty"`
	Error         string                 `json:"error,omitempty"`
	RewardAmount  *big.Int               `json:"reward_amount,omitempty"`
	RetryCount    int                    `json:"retry_count"`
	MaxRetries    int                    `json:"max_retries"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
}

func NewClaimExecutor() *ClaimExecutor {
	return &ClaimExecutor{
		claims:  make(map[string]*ClaimJob),
		stopCh:  make(chan struct{}),
	}
}

func (c *ClaimExecutor) SetWalletFunder(wf *WalletFunder) {
	c.walletFunder = wf
}

func (c *ClaimExecutor) SetClient(chain string, client *ethclient.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.clients == nil {
		c.clients = make(map[string]*ethclient.Client)
	}
	c.clients[chain] = client
}

func (c *ClaimExecutor) Run(ctx context.Context) {
	c.mu.Lock()
	if c.running {
		return
	}
	c.running = true
	c.stopCh = make(chan struct{})
	c.mu.Unlock()

	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-c.stopCh:
				return
			case <-ticker.C:
				c.processPendingClaims()
			}
		}
	}()
	log.Println("[CLAIM] Claim Executor started")
}

func (c *ClaimExecutor) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		close(c.stopCh)
		c.running = false
		log.Println("[CLAIM] Claim Executor stopped")
	}
}

// ScheduleClaim schedules a claim for when mainnet launches
func (c *ClaimExecutor) ScheduleClaim(job *ClaimJob) error {
	if job.ID == "" {
		job.ID = generateClaimID()
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}
	job.UpdatedAt = time.Now()
	job.Status = "pending"
	job.RetryCount = 0
	job.MaxRetries = 5

	c.mu.Lock()
	c.claims[job.ID] = job
	c.mu.Unlock()

	log.Printf("[CLAIM] Scheduled claim %s for campaign %s on %s", job.ID, job.CampaignID, job.Chain)
	return nil
}

// GetClaim returns a claim job by ID
func (c *ClaimExecutor) GetClaim(id string) *ClaimJob {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.claims[id]
}

// GetAllClaims returns all claim jobs
func (c *ClaimExecutor) GetAllClaims() []*ClaimJob {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]*ClaimJob, 0, len(c.claims))
	for _, c := range c.claims {
		result = append(result, c)
	}
	return result
}

// ProcessPendingClaims checks and executes due claims
func (c *ClaimExecutor) processPendingClaims() {
	c.mu.RLock()
	var pending []*ClaimJob
	for _, claim := range c.claims {
		if claim.Status == "pending" || claim.Status == "scheduled" {
			if time.Until(claim.ScheduledAt) <= 0 {
				pending = append(pending, claim)
			}
		}
	}
	c.mu.RUnlock()

	for _, claim := range pending {
		go c.executeClaim(claim)
	}
}

// ExecuteClaim executes a claim transaction
func (c *ClaimExecutor) executeClaim(job *ClaimJob) {
	c.mu.Lock()
	job.Status = "executing"
	job.UpdatedAt = time.Now()
	job.RetryCount++
	c.mu.Unlock()

	log.Printf("[CLAIM] Executing claim %s for campaign %s", job.ID, job.CampaignID)

	// Get client for chain
	c.mu.RLock()
	client, ok := c.clients[job.Chain]
	c.mu.RUnlock()

	if !ok {
		c.markFailed(job, fmt.Errorf("no client for chain %s", job.Chain))
		return
	}

	// Get wallet private key (would come from secure storage in production)
	privateKey := c.getWalletPrivateKey(job.WalletAddress)
	if privateKey == nil {
		c.markFailed(job, fmt.Errorf("no private key for wallet %s", job.WalletAddress))
		return
	}

	// Parse claim contract ABI
	parsedABI, err := parseClaimABI(job.ClaimABI)
	if err != nil {
		c.markFailed(job, fmt.Errorf("invalid ABI: %v", err))
		return
	}

	// Create contract instance
	contract := bind.NewBoundContract(common.HexToAddress(job.ClaimContract), parsedABI, client, client, client)

	// Prepare transaction
	auth, err := c.createAuth(job.WalletAddress, job.Chain)
	if err != nil {
		c.markFailed(job, fmt.Errorf("failed to create auth: %v", err))
		return
	}

	// Call claim method
	tx, err := contract.Transact(auth, job.ClaimMethod, job.MethodParams...)
	if err != nil {
		c.markFailed(job, fmt.Errorf("claim failed: %v", err))
		return
	}

	// Wait for receipt
	receipt, err := bind.WaitMined(context.Background(), c.getClient(job.Chain), tx)
	if err != nil {
		c.markFailed(job, fmt.Errorf("wait mined failed: %v", err))
		return
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		c.markFailed(job, fmt.Errorf("transaction reverted"))
		return
	}

	// Success
	c.mu.Lock()
	job.Status = "completed"
	job.ExecutedAt = timePtr(time.Now())
	job.TxHash = tx.Hash().Hex()
	job.UpdatedAt = time.Now()
	c.mu.Unlock()

	log.Printf("[CLAIM] ✅ Claim %s successful: %s", job.ID, tx.Hash().Hex())
}

// Helper methods
func (c *ClaimExecutor) markFailed(job *ClaimJob, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	job.Error = err.Error()
	job.UpdatedAt = time.Now()

	if job.RetryCount < job.MaxRetries {
		job.Status = "pending"
		// Exponential backoff
		job.ScheduledAt = time.Now().Add(time.Duration(job.RetryCount*2) * time.Hour)
		log.Printf("[CLAIM] ❌ Claim %s failed (attempt %d/%d): %v - retrying in %dh",
			job.ID, job.RetryCount, job.MaxRetries, err, job.RetryCount*2)
	} else {
		job.Status = "failed"
		log.Printf("[CLAIM] 💀 Claim %s failed permanently after %d retries: %v",
			job.ID, job.MaxRetries, err)
	}
}

func (c *ClaimExecutor) getWalletPrivateKey(address string) *ecdsa.PrivateKey {
	// In production, fetch from secure vault/HSM
	// For now, return nil - would integrate with secure key management
	return nil
}

func (c *ClaimExecutor) getClient(chain string) *ethclient.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.clients[chain]
}

func (c *ClaimExecutor) createAuth(walletAddress, chain string) (*bind.TransactOpts, error) {
	privateKey := c.getWalletPrivateKey(walletAddress)
	if privateKey == nil {
		return nil, fmt.Errorf("no private key for wallet")
	}

	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, big.NewInt(c.getChainID(chain)))
	if err != nil {
		return nil, err
	}

	auth.GasLimit = 500000
	auth.GasPrice = big.NewInt(20000000000) // 20 gwei
	return auth, nil
}

func (c *ClaimExecutor) getChainID(chain string) int64 {
	chainIDs := map[string]int64{
		"ethereum":   1,
		"arbitrum":   42161,
		"optimism":   10,
		"base":       8453,
		"polygon":    137,
		"scroll":     534352,
		"linea":      59144,
		"zksync":     324,
		"arbitrum-nova": 42170,
	}
	if id, ok := chainIDs[chain]; ok {
		return id
	}
	return 1
}

func parseClaimABI(abiJSON string) (abi.ABI, error) {
	return abi.JSON(strings.NewReader(abiJSON))
}

// GetPendingClaims returns all pending claims
func (c *ClaimExecutor) GetPendingClaims() []*ClaimJob {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []*ClaimJob
	for _, claim := range c.claims {
		if claim.Status == "pending" || claim.Status == "scheduled" {
			result = append(result, claim)
		}
	}
	return result
}

// GetClaimHistory returns claim history for a campaign
func (c *ClaimExecutor) GetClaimHistory(campaignID string) []*ClaimJob {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []*ClaimJob
	for _, claim := range c.claims {
		if claim.CampaignID == campaignID {
			result = append(result, claim)
		}
	}
	return result
}

// CancelClaim cancels a pending claim
func (c *ClaimExecutor) CancelClaim(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	job, ok := c.claims[id]
	if !ok {
		return fmt.Errorf("claim not found: %s", id)
	}

	if job.Status != "pending" && job.Status != "scheduled" {
		return fmt.Errorf("cannot cancel claim in status: %s", job.Status)
	}

	job.Status = "cancelled"
	job.UpdatedAt = time.Now()
	return nil
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

func timePtr(t time.Time) *time.Time {
	return &t
}

func generateClaimID() string {
	return fmt.Sprintf("claim-%s-%d", randomString(8), time.Now().Unix())
}