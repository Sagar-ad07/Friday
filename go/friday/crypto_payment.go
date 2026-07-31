package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type CryptoPaymentSystem struct {
	mu              sync.RWMutex
	walletAddress   string
	network         string
	apiKey          string
	secretKey       string
	subscriptions   map[string]CryptoSubscription
	totalReceived   float64
	payments        []Payment
	pollInterval    time.Duration
}

type CryptoSubscription struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	Tier        string    `json:"tier"`
	Amount      float64   `json:"amount"`
	Token       string    `json:"token"`
	TxHash      string    `json:"tx_hash"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	ActivatedAt time.Time `json:"activated_at"`
}

type Payment struct {
	TxHash    string    `json:"tx_hash"`
	From      string    `json:"from"`
	Amount    float64   `json:"amount"`
	Token     string    `json:"token"`
	Timestamp time.Time `json:"timestamp"`
	Status    string    `json:"status"`
}

var cryptoPaymentSystem *CryptoPaymentSystem
var cryptoPaymentOnce sync.Once

func GetCryptoPaymentSystem() *CryptoPaymentSystem {
	cryptoPaymentOnce.Do(func() {
		cryptoPaymentSystem = &CryptoPaymentSystem{
			walletAddress: os.Getenv("FRIDAY_WALLET_ADDRESS"),
			network:       "polygon",
			pollInterval:  60 * time.Second,
			subscriptions: map[string]CryptoSubscription{},
			totalReceived: 0,
			payments:      []Payment{},
		}
	})
	return cryptoPaymentSystem
}

func (cps *CryptoPaymentSystem) Start(ctx context.Context) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[CRYPTO-PAYMENT] crashed: %v — restarting", r)
				time.Sleep(30 * time.Second)
				cps.Start(ctx)
			}
		}()

		walletDisplay := cps.walletAddress
		if len(walletDisplay) > 10 {
			walletDisplay = walletDisplay[:10] + "..."
		} else if len(walletDisplay) > 0 {
			walletDisplay = walletDisplay + "..."
		} else {
			walletDisplay = "unset..."
		}
		log.Printf("[CRYPTO-PAYMENT] payment scanner started on %s wallet %s", cps.network, walletDisplay)

		for {
			select {
			case <-ctx.Done():
				return
			default:
				cps.scanWallet(ctx)
				time.Sleep(cps.pollInterval)
			}
		}
	}()
}

func (cps *CryptoPaymentSystem) scanWallet(ctx context.Context) {
	if cps.walletAddress == "" {
		return
	}

	apiKey := os.Getenv("POLYGONSCAN_API_KEY")
	if apiKey == "" {
		return
	}

	usdcContract := "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174"
	urlStr := fmt.Sprintf("https://api.polygonscan.com/api?module=account&action=tokentx&contractaddress=%s&address=%s&startblock=0&endblock=99999999&sort=asc&apikey=%s",
		url.QueryEscape(usdcContract),
		url.QueryEscape(cps.walletAddress),
		url.QueryEscape(apiKey))

	req, _ := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	_ = json.Unmarshal(body, &result)

	status, _ := result["status"].(string)
	if status != "1" {
		return
	}

	if result["result"] == nil {
		return
	}

	txList, ok := result["result"].([]any)
	if !ok {
		return
	}

	for i := len(txList) - 1; i >= 0; i-- {
		tx, ok := txList[i].(map[string]any)
		if !ok {
			continue
		}

		txHash, _ := tx["txHash"].(string)
		from, _ := tx["from"].(string)
		to, _ := tx["to"].(string)
		valueRaw, _ := tx["value"].(string)

		var value float64
		if v, err := parseBigInt(valueRaw); err == nil {
			value = v / 1e6
		}

		timestampRaw, _ := tx["timeStamp"].(string)
		timestampF := time.Now()
		if ts, err := time.Parse("2006-01-02 15:04:05", timestampRaw); err == nil {
			timestampF = ts
		}

		toCompare := to
		if toCompare == "" {
			toCompare = cps.walletAddress
		}

		if toCompare != "" && strings.EqualFold(cps.walletAddress, toCompare) && value > 0.5 {
			cps.processPayment(context.Background(), Payment{
				TxHash:    txHash,
				From:      from,
				Amount:    value,
				Token:     "USDC",
				Timestamp: timestampF,
				Status:    "received",
			})
		}
	}
}

func parseBigInt(s string) (float64, error) {
	var result float64
	for _, ch := range s {
		if ch >= '0' && ch <= '9' {
			result = result*10 + float64(ch-'0')
		} else if ch == '\n' || ch == '\r' || ch == ' ' {
			continue
		} else {
			break
		}
	}
	return result, nil
}

func (cps *CryptoPaymentSystem) processPayment(ctx context.Context, payment Payment) {
	cps.mu.Lock()
	defer cps.mu.Unlock()

	for _, existing := range cps.payments {
		if existing.TxHash == payment.TxHash {
			return
		}
	}

	cps.payments = append(cps.payments, payment)
	cps.totalReceived += payment.Amount

	log.Printf("[CRYPTO-PAYMENT] received %.2f USDC from %s tx=%s", payment.Amount, maskAddr(payment.From), payment.TxHash[:16]+"...")

	for id, sub := range cps.subscriptions {
		if sub.Status != "pending" {
			continue
		}
		expectedAmount := 299.99
		if sub.Tier == "enterprise" {
			expectedAmount = 999.99
		} else if sub.Tier == "standard" {
			expectedAmount = 99.99
		}

		if payment.Amount >= expectedAmount*0.95 {
			sub.Status = "active"
			sub.ActivatedAt = time.Now()
			cps.subscriptions[id] = sub
			log.Printf("[CRYPTO-PAYMENT] subscription %s ACTIVATED (tier=%s amount=%.2f)", id, sub.Tier, payment.Amount)
		}
	}
}

func maskAddr(addr string) string {
	if len(addr) < 10 {
		return addr
	}
	return addr[:6] + "..." + addr[len(addr)-4:]
}

func (cps *CryptoPaymentSystem) CreatePendingSubscription(email string, tier string) (string, CryptoSubscription) {
	cps.mu.Lock()
	defer cps.mu.Unlock()

	subID := hashString(email + time.Now().Format(time.RFC3339))
	expectedAmount := 299.99
	if tier == "enterprise" {
		expectedAmount = 999.99
	} else if tier == "standard" {
		expectedAmount = 99.99
	}

	subscription := CryptoSubscription{
		ID:        subID,
		Email:     email,
		Tier:      tier,
		Amount:    expectedAmount,
		Token:     "USDC",
		Status:    "pending",
		CreatedAt: time.Now(),
	}

	cps.subscriptions[subID] = subscription

	return subID, subscription
}

func (cps *CryptoPaymentSystem) GetSubscriptionStatus(subID string) (CryptoSubscription, bool) {
	cps.mu.RLock()
	defer cps.mu.RUnlock()

	sub, ok := cps.subscriptions[subID]
	return sub, ok
}

func (cps *CryptoPaymentSystem) GetTotalReceived() float64 {
	cps.mu.RLock()
	defer cps.mu.RUnlock()
	return cps.totalReceived
}

func (cps *CryptoPaymentSystem) GetStats() map[string]any {
	cps.mu.RLock()
	defer cps.mu.RUnlock()

	activeCount := 0
	pendingCount := 0
	for _, sub := range cps.subscriptions {
		if sub.Status == "active" {
			activeCount++
		} else if sub.Status == "pending" {
			pendingCount++
		}
	}

	walletDisplay := cps.walletAddress
	if len(walletDisplay) > 10 {
		walletDisplay = walletDisplay[:10] + "..."
	} else if len(walletDisplay) > 0 {
		walletDisplay = walletDisplay + "..."
	} else {
		walletDisplay = "unset..."
	}

	return map[string]any{
		"wallet_address":       walletDisplay,
		"network":              cps.network,
		"total_received_usdc":  cps.totalReceived,
		"total_subscriptions":  len(cps.subscriptions),
		"active_subscriptions": activeCount,
		"pending_subscriptions": pendingCount,
		"payment_count":        len(cps.payments),
	}
}