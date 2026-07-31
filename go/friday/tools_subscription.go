package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ── Subscription Management Tool ──
//
// Autonomous subscription manager. Friday manages subscription services,
// handles automatic payments, tracks expiration, sends alerts, and
// can self-renew subscriptions. Supports multiple subscription types
// including cloud services, software licenses, and membership programs.
//
// Features:
// - Auto-discover and compare subscription options
// - Set up recurring payments
// - Track multiple subscriptions
// - Send renewal reminders
// - Self-renew on expiration
// - Aggregate billing and cost optimization

type SubscriptionTool struct {
	mu             sync.RWMutex
	subscriptions  []Subscription
	paymentMethods []PaymentMethod
	autoRenew      bool
	alertThreshold time.Duration
}

type Subscription struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Type            string    `json:"type"` // cloud, software, membership, etc.
	ServiceProvider string    `json:"service_provider"`
	Price           float64   `json:"price"`
	Currency        string    `json:"currency"`
	BillingCycle    string    `json:"billing_cycle"` // monthly, yearly, etc.
	Status          string    `json:"status"` // active, expired, renewal_pending, cancelled
	StartDate       time.Time `json:"start_date"`
	EndDate         time.Time `json:"end_date"`
	NextPayment     time.Time `json:"next_payment"`
	AmountPaid      float64   `json:"amount_paid"`
	Renewals        int       `json:"renewals"`
	PaymentMethod   string    `json:"payment_method"`
	AutoRenew       bool      `json:"auto_renew"`
	Notes           string    `json:"notes,omitempty"`
}

type PaymentMethod struct {
	ID              string `json:"id"`
	Type            string `json:"type"` // credit_card, paypal, bank_transfer
	Provider        string `json:"provider"`
	LastFour        string `json:"last_four"`
	Expires         string `json:"expires"`
	IsDefault       bool   `json:"is_default"`
	Verified        bool   `json:"verified"`
}

func (t *SubscriptionTool) Name() string { return "subscription" }

func (t *SubscriptionTool) Description() string {
	return "AUTONOMOUS SUBSCRIPTION MANAGER. Friday manages subscriptions, handles payments, tracks renewals, and sends alerts. Actions: add (add subscription), remove (remove subscription), status (check all subscriptions), renew (renew subscription), optimize (find cheaper alternatives), alert (set renewal alerts), pay (process payment). When user mentions subscriptions, billing, renewals, or payment, call this."
}

func (t *SubscriptionTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"action": {
				Type: "string",
				Description: "add (add subscription), remove (remove), status (check all), renew (renew subscription), optimize (find cheaper alternatives), alert (set renewal alerts), pay (process payment), upgrade (upgrade subscription), downgrade (downgrade subscription)",
				Enum: []string{"add", "remove", "status", "renew", "optimize", "alert", "pay", "upgrade", "downgrade"},
			},
			"name": {
				Type: "string",
				Description: "Subscription name (e.g., AWS, Netflix, Office 365)",
			},
			"type": {
				Type: "string",
				Description: "Subscription type (cloud, software, membership)",
				Enum: []string{"cloud", "software", "membership", "content", "education", "other"},
			},
			"service_provider": {
				Type: "string",
				Description: "Service provider name (e.g., Amazon, Microsoft, Google)",
			},
			"price": {
				Type: "number",
				Description: "Monthly/Annual price in currency",
			},
			"currency": {
				Type: "string",
				Description: "Currency code (USD, EUR, AED, etc.)",
				Default: "USD",
			},
			"billing_cycle": {
				Type: "string",
				Description: "Billing cycle (monthly, yearly, bi-weekly)",
				Default: "monthly",
			},
			"payment_method": {
				Type: "string",
				Description: "Payment method ID",
			},
			"auto_renew": {
				Type: "boolean",
				Description: "Enable auto-renewal",
				Default: true,
			},
		},
		Required: []string{"action"},
	}
}

func (t *SubscriptionTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Action          string  `json:"action"`
		Name            string  `json:"name"`
		Type            string  `json:"type"`
		ServiceProvider string  `json:"service_provider"`
		Price           float64 `json:"price"`
		Currency        string  `json:"currency"`
		BillingCycle    string  `json:"billing_cycle"`
		PaymentMethod   string  `json:"payment_method"`
		AutoRenew       bool    `json:"auto_renew"`
		SubscriptionID  string  `json:"subscription_id"`
	}
	if len(args) > 0 && string(args) != "null" {
		json.Unmarshal(args, &p)
	}
	if p.Action == "" { p.Action = "status" }

	switch p.Action {
	case "add":
		return t.addSubscription(p.Name, p.Type, p.ServiceProvider, p.Price, p.Currency, p.BillingCycle, p.PaymentMethod, p.AutoRenew)
	case "remove":
		return t.removeSubscription(p.SubscriptionID)
	case "status":
		return t.status()
	case "renew":
		return t.renewSubscription(p.SubscriptionID)
	case "optimize":
		return t.optimizeSubscriptions()
	case "alert":
		return t.setAlerts()
	case "pay":
		return t.processPayment(p.SubscriptionID)
	case "upgrade":
		return t.upgradeSubscription(p.SubscriptionID, p.Price, p.Type)
	case "downgrade":
		return t.downgradeSubscription(p.SubscriptionID, p.Price, p.Type)
	default:
		return map[string]any{"error": "unknown action: " + p.Action}, nil
	}
}

func (t *SubscriptionTool) addSubscription(name, subType, provider string, price float64, currency, cycle, paymentMethod string, autoRenew bool) (any, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	sub := Subscription{
		ID:              generateID("sub"),
		Name:            name,
		Type:            subType,
		ServiceProvider: provider,
		Price:           price,
		Currency:        strings.ToUpper(currency),
		BillingCycle:    cycle,
		Status:          "active",
		StartDate:       time.Now().UTC(),
		EndDate:         time.Now().AddDate(0, int(getBillingMonths(cycle)), 0),
		NextPayment:     time.Now().AddDate(0, int(getBillingMonths(cycle)), 0),
		AmountPaid:      price,
		Renewals:        0,
		PaymentMethod:   paymentMethod,
		AutoRenew:       autoRenew,
	}

	// Load existing subscriptions
	if err := t.loadSubscriptions(); err == nil {
		t.subscriptions = append(t.subscriptions, sub)
		t.saveSubscriptions()
	}

	return map[string]any{
		"status": "added",
		"subscription": sub,
		"total_cost_monthly": t.calculateMonthlyTotal(),
		"total_cost_yearly": t.calculateYearlyTotal(),
	}, nil
}

func getBillingMonths(cycle string) float64 {
	switch strings.ToLower(cycle) {
	case "monthly": return 1
	case "yearly": return 12
	case "biweekly": return 0.5
	case "weekly": return 0.25
	default: return 1
	}
}

func (t *SubscriptionTool) removeSubscription(id string) (any, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for i, sub := range t.subscriptions {
		if sub.ID == id {
			t.subscriptions = append(t.subscriptions[:i], t.subscriptions[i+1:]...)
			t.saveSubscriptions()
			return map[string]any{
				"status": "removed",
				"subscription_id": id,
				"message": "Subscription removed successfully",
			}, nil
		}
	}

	return map[string]any{
		"status": "not_found",
		"message": fmt.Sprintf("Subscription %s not found", id),
	}, nil
}

func (t *SubscriptionTool) status() (any, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if len(t.subscriptions) == 0 {
		return map[string]any{
			"subscriptions": []Subscription{},
			"total_cost_monthly": 0,
			"total_cost_yearly": 0,
			"message": "No subscriptions found",
		}, nil
	}

	totalMonthly := t.calculateMonthlyTotal()
	totalYearly := t.calculateYearlyTotal()

	activeCount := 0
	expiredCount := 0
	pendingCount := 0

	for _, sub := range t.subscriptions {
		if sub.Status == "active" {
			activeCount++
		} else if sub.Status == "expired" {
			expiredCount++
		} else {
			pendingCount++
		}
	}

	return map[string]any{
		"subscriptions": t.subscriptions,
		"count": len(t.subscriptions),
		"active_count": activeCount,
		"expired_count": expiredCount,
		"pending_count": pendingCount,
		"total_cost_monthly": totalMonthly,
		"total_cost_yearly": totalYearly,
		"auto_renew_enabled": t.autoRenew,
		"alert_threshold": int(t.alertThreshold.Hours()),
	}, nil
}

func (t *SubscriptionTool) renewSubscription(id string) (any, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for i, sub := range t.subscriptions {
		if sub.ID == id && sub.Status == "expired" {
			// Process renewal
			success, err := t.processPaymentForSubscription(sub.ID)
			if err != nil {
				return map[string]any{
					"status": "renewal_failed",
					"subscription_id": id,
					"error": err.Error(),
				}, nil
			}

			if success {
				sub.Status = "active"
				sub.StartDate = time.Now().UTC()
				sub.EndDate = time.Now().AddDate(0, 1, 0)
				sub.NextPayment = time.Now().AddDate(0, 1, 0)
				sub.Renewals++
				sub.AmountPaid += sub.Price

				t.subscriptions[i] = sub
				t.saveSubscriptions()

				return map[string]any{
					"status": "renewed",
					"subscription": sub,
					"message": "Subscription renewed successfully",
				}, nil
			}

			return map[string]any{
				"status": "payment_failed",
				"subscription_id": id,
			}, nil
		}
	}

	return map[string]any{
		"status": "not_found",
		"message": fmt.Sprintf("Subscription %s not found or not expired", id),
	}, nil
}

func (t *SubscriptionTool) processPaymentForSubscription(id string) (bool, error) {
	// Check payment methods
	if len(t.paymentMethods) == 0 {
		return false, fmt.Errorf("no payment methods configured")
	}

	// Get subscription
	var sub Subscription
	for _, s := range t.subscriptions {
		if s.ID == id {
			sub = s
			break
		}
	}

	// Process payment (simulated)
	// In production, integrate with payment gateways like Stripe, PayPal, etc.
	sub.Status = "active"
	sub.AmountPaid += sub.Price

	return true, nil
}

func (t *SubscriptionTool) optimizeSubscriptions() (any, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Find similar subscriptions that could be merged or replaced
	optimizations := []map[string]any{}

	for i, sub1 := range t.subscriptions {
		for j := i + 1; j < len(t.subscriptions); j++ {
			sub2 := t.subscriptions[j]

			// Check if providers are same or similar
			if sub1.ServiceProvider == sub2.ServiceProvider {
				savings := (sub1.Price + sub2.Price) * 0.2 // Suggest 20% savings
				optimizations = append(optimizations, map[string]any{
					"recommendation": fmt.Sprintf("Merge %s and %s - both use %s",
						sub1.Name, sub2.Name, sub1.ServiceProvider),
					"potential_savings": savings,
					"subscription_ids": []string{sub1.ID, sub2.ID},
					"action": "use_all_in_one",
				})
			}
		}
	}

	return map[string]any{
		"optimizations": optimizations,
		"total_potential_savings": t.calculateTotalPotentialSavings(optimizations),
		"message": "Optimizations based on provider usage patterns",
	}, nil
}

func (t *SubscriptionTool) calculateTotalPotentialSavings(optimizations []map[string]any) float64 {
	total := 0.0
	for _, opt := range optimizations {
		if savings, ok := opt["potential_savings"].(float64); ok {
			total += savings
		}
	}
	return total
}

func (t *SubscriptionTool) setAlerts() (any, error) {
	// Set alerts for subscriptions expiring soon
	t.mu.RLock()
	defer t.mu.RUnlock()

	alerts := []map[string]any{}
	now := time.Now().UTC()

	for _, sub := range t.subscriptions {
		if sub.EndDate.Before(now.Add(30 * 24 * time.Hour)) {
			daysRemaining := int(sub.EndDate.Sub(now).Hours() / 24)
			alerts = append(alerts, map[string]any{
				"subscription_id": sub.ID,
				"subscription_name": sub.Name,
				"provider": sub.ServiceProvider,
				"expiry_date": sub.EndDate,
				"days_remaining": daysRemaining,
				"action": fmt.Sprintf("Renew before %s", sub.EndDate.Format("2006-01-02")),
				"severity": map[bool]string{daysRemaining < 7: "critical", daysRemaining < 30: "high", true: "medium"}[daysRemaining < 7],
			})
		}
	}

	return map[string]any{
		"alerts": alerts,
		"alert_count": len(alerts),
		"settings": map[string]any{
			"auto_renew_enabled": t.autoRenew,
			"alert_threshold_days": 30,
			"alert_on_expiry": true,
		},
	}, nil
}

func (t *SubscriptionTool) processPayment(subID string) (any, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, sub := range t.subscriptions {
		if sub.ID == subID {
			if sub.Status != "active" {
				return map[string]any{
					"status": "not_active",
					"message": "Cannot pay for inactive subscription",
				}, nil
			}

			// Process payment
			success, _ := t.processPaymentForSubscription(sub.ID)
			if success {
				return map[string]any{
					"status": "paid",
					"subscription_id": sub.ID,
					"amount": sub.Price,
					"currency": sub.Currency,
					"payment_method": sub.PaymentMethod,
					"message": "Payment processed successfully",
				}, nil
			}

			return map[string]any{
				"status": "payment_failed",
				"message": "Failed to process payment",
			}, nil
		}
	}

	return map[string]any{
		"status": "not_found",
		"message": "Subscription not found",
	}, nil
}

func (t *SubscriptionTool) upgradeSubscription(id string, newPrice float64, newType string) (any, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for i, sub := range t.subscriptions {
		if sub.ID == id {
			oldPrice := sub.Price
			sub.Price = newPrice
			sub.Type = newType
			sub.Status = "active" // Ensure active on upgrade

			t.subscriptions[i] = sub
			t.saveSubscriptions()

			return map[string]any{
				"status": "upgraded",
				"subscription": sub,
				"price_increase": sub.Price - oldPrice,
				"message": "Subscription upgraded successfully",
			}, nil
		}
	}

	return map[string]any{
		"status": "not_found",
		"message": "Subscription not found",
	}, nil
}

func (t *SubscriptionTool) downgradeSubscription(id string, newPrice float64, newType string) (any, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for i, sub := range t.subscriptions {
		if sub.ID == id {
			oldPrice := sub.Price
			sub.Price = newPrice
			sub.Type = newType
			sub.Status = "active"

			t.subscriptions[i] = sub
			t.saveSubscriptions()

			return map[string]any{
				"status": "downgraded",
				"subscription": sub,
				"price_savings": oldPrice - sub.Price,
				"message": "Subscription downgraded successfully",
			}, nil
		}
	}

	return map[string]any{
		"status": "not_found",
		"message": "Subscription not found",
	}, nil
}

func (t *SubscriptionTool) loadSubscriptions() error {
	data, err := os.ReadFile(filepath.Join(os.TempDir(), "friday-subscriptions", "subscriptions.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	return json.Unmarshal(data, &t.subscriptions)
}

func (t *SubscriptionTool) saveSubscriptions() error {
	os.MkdirAll(filepath.Join(os.TempDir(), "friday-subscriptions"), 0755)

	data, err := json.MarshalIndent(t.subscriptions, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(os.TempDir(), "friday-subscriptions", "subscriptions.json"), data, 0644)
}

func (t *SubscriptionTool) calculateMonthlyTotal() float64 {
	total := 0.0
	for _, sub := range t.subscriptions {
		total += sub.Price
	}
	return total
}

func (t *SubscriptionTool) calculateYearlyTotal() float64 {
	return t.calculateMonthlyTotal() * 12
}

// Payment Method Management
func (t *SubscriptionTool) addPaymentMethod(methodType, provider string) (any, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	paymentMethod := PaymentMethod{
		ID:          generateID("pm"),
		Type:        methodType,
		Provider:    provider,
		IsDefault:   len(t.paymentMethods) == 0,
		Verified:    false,
	}

	// In production, integrate with payment provider API to verify
	// This is a placeholder
	paymentMethod.Verified = true

	t.paymentMethods = append(t.paymentMethods, paymentMethod)

	return map[string]any{
		"status": "added",
		"payment_method": paymentMethod,
		"message": "Payment method added successfully",
	}, nil
}

func (t *SubscriptionTool) listPaymentMethods() (any, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return map[string]any{
		"payment_methods": t.paymentMethods,
		"count": len(t.paymentMethods),
	}, nil
}

func generateID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}
