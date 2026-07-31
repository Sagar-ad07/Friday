package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type MarketingEngine struct {
	mu                   sync.RWMutex
	discoveredCommunities []Community
	freeSignalsPosted    int
	paidConversions      int
	lastOutreach         time.Time
	outreachInterval     time.Duration
}

type Community struct {
	ID         string
	Platform   string
	Name       string
	URL        string
	Members    int
	Active     bool
	LastPosted time.Time
	FreePosts  int
	PaidConv   int
}

var marketingEngine *MarketingEngine
var marketingOnce sync.Once

func GetMarketingEngine() *MarketingEngine {
	marketingOnce.Do(func() {
		marketingEngine = &MarketingEngine{
			discoveredCommunities: []Community{},
			outreachInterval:      4 * time.Hour,
		}
	})
	return marketingEngine
}

func (me *MarketingEngine) Start(ctx context.Context) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[MARKETING] crashed: %v — restarting", r)
				time.Sleep(60 * time.Second)
				me.Start(ctx)
			}
		}()

		log.Printf("[MARKETING] autonomous marketing engine started")
		me.discoverCommunities(ctx)

		ticker := time.NewTicker(me.outreachInterval)
		defer ticker.Stop()

		me.runOutreachCycle(ctx)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				me.runOutreachCycle(ctx)
			}
		}
	}()
}

func (me *MarketingEngine) discoverCommunities(ctx context.Context) {
	log.Printf("[MARKETING] discovering crypto communities...")
	me.searchAndAdd(ctx, "crypto discord servers 2024", "discord")
	me.searchAndAdd(ctx, "crypto telegram groups official active", "telegram")
	me.searchAndAdd(ctx, "crypto trading subreddit active", "reddit")
}

func (me *MarketingEngine) searchAndAdd(ctx context.Context, query string, platform string) {
	apiKey := os.Getenv("BRAVE_SEARCH_API_KEY")
	if apiKey == "" {
		return
	}

	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=5", url.QueryEscape(query)), nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return
	}

	webResults, ok := data["web"].([]any)
	if !ok {
		return
	}

	for _, r := range webResults {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		titleRaw, _ := m["title"]
		title, _ := titleRaw.(string)
		urlRaw, _ := m["url"]
		urlStr, _ := urlRaw.(string)

		if title == "" || urlStr == "" {
			continue
		}

		id := hashString(title + platform)

		me.mu.Lock()
		already := false
		for _, c := range me.discoveredCommunities {
			if c.ID == id {
				already = true
				break
			}
		}
		if !already {
			me.discoveredCommunities = append(me.discoveredCommunities, Community{
				ID:       id,
				Platform: platform,
				Name:     title,
				URL:      urlStr,
				Active:   true,
			})
			log.Printf("[MARKETING] discovered community: %s (%s)", title[:50], platform)
		}
		me.mu.Unlock()
	}
}

func (me *MarketingEngine) runOutreachCycle(ctx context.Context) {
	log.Printf("[MARKETING] running outreach cycle...")
	me.lastOutreach = time.Now()

	pipeline := GetSignalPipeline()
	highSignals := pipeline.GetHighConfidenceSignals()

	if len(highSignals) == 0 {
		log.Printf("[MARKETING] no high-confidence signals — skipping outreach")
		return
	}

	me.mu.Lock()
	communities := make([]Community, len(me.discoveredCommunities))
	copy(communities, me.discoveredCommunities)
	me.mu.Unlock()

	sort.Slice(communities, func(i, j int) bool {
		return communities[i].LastPosted.Before(communities[j].LastPosted)
	})

	posted := 0
	for _, community := range communities {
		if posted >= 3 {
			break
		}
		if !community.Active {
			continue
		}
		if time.Since(community.LastPosted) < 48*time.Hour {
			continue
		}

		success := me.postToCommunity(ctx, community, highSignals)
		if success {
			posted++
			me.mu.Lock()
			for i, c := range me.discoveredCommunities {
				if c.ID == community.ID {
					c.LastPosted = time.Now()
					c.FreePosts++
					me.discoveredCommunities[i] = c
					break
				}
			}
			me.freeSignalsPosted++
			me.mu.Unlock()
			log.Printf("[MARKETING] posted to %s (%s)", community.Name[:40], community.Platform)
		}
	}

	log.Printf("[MARKETING] outreach cycle done: posted to %d communities", posted)
}

func (me *MarketingEngine) postToCommunity(ctx context.Context, community Community, signals []TradeSignal) bool {
	switch community.Platform {
	case "discord":
		return me.postToDiscord(ctx, community, signals)
	case "telegram":
		return me.postToTelegram(ctx, community, signals)
	case "reddit":
		return me.postToReddit(ctx, community, signals)
	}
	return false
}

func (me *MarketingEngine) postToDiscord(ctx context.Context, community Community, signals []TradeSignal) bool {
	webhookURL := os.Getenv("DISCORD_WEBHOOK_URL")
	if webhookURL == "" {
		return false
	}

	embed := me.buildDiscordEmbed(signals)
	payload := map[string]any{
		"embeds": []any{embed},
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequestWithContext(ctx, "POST", webhookURL, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == 204 || resp.StatusCode == 200
}

func (me *MarketingEngine) postToTelegram(ctx context.Context, community Community, signals []TradeSignal) bool {
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	chatID := os.Getenv("TELEGRAM_CHAT_ID")
	if botToken == "" || chatID == "" {
		return false
	}

	message := me.buildTelegramMessage(signals)
	reqURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	payload := map[string]any{
		"chat_id":    chatID,
		"text":       message,
		"parse_mode": "HTML",
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequestWithContext(ctx, "POST", reqURL, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == 200
}

func (me *MarketingEngine) postToReddit(ctx context.Context, community Community, signals []TradeSignal) bool {
	log.Printf("[MARKETING] would post to Reddit community: %s", community.Name[:40])
	return false
}

func (me *MarketingEngine) buildDiscordEmbed(signals []TradeSignal) map[string]any {
	if len(signals) == 0 {
		return map[string]any{}
	}

	signalLines := []string{}
	for i, sig := range signals {
		if i >= 3 {
			break
		}
		emoji := "🟢"
		if sig.Direction == "SELL" {
			emoji = "🔴"
		}
		signalLines = append(signalLines, fmt.Sprintf("%s **%s** %s | Confidence: %.0f%% | Win Rate: %.1f%%",
			emoji, sig.Symbol, sig.Direction, sig.Confidence*100, sig.WinRate))
	}

	description := "🤖 **Friday AI Signals** — Autonomously generated by Friday\n\n"
	description += strings.Join(signalLines, "\n")
	description += "\n\nSubscribe for real-time signals: `https://friday.ai/signals/subscribe`"

	return map[string]any{
		"title":       fmt.Sprintf("Friday Signal Alert — %s", signals[0].Symbol),
		"description": description,
		"color":       3447003,
		"footer": map[string]any{
			"text": fmt.Sprintf("Friday Autonomous Signal Engine | %d signals today", len(signals)),
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}
}

func (me *MarketingEngine) buildTelegramMessage(signals []TradeSignal) string {
	if len(signals) == 0 {
		return "No signals available."
	}

	msg := "<b>🤖 Friday AI Trading Signals</b>\n\n"
	for i, sig := range signals {
		if i >= 3 {
			break
		}
		emoji := "🟢"
		if sig.Direction == "SELL" {
			emoji = "🔴"
		}
		msg += fmt.Sprintf("%s <b>%s</b> %s\nConfidence: %.0f%% | Win Rate: %.1f%%\n\n",
			emoji, sig.Symbol, sig.Direction, sig.Confidence*100, sig.WinRate)
	}
	msg += "🔗 Subscribe for real-time signals: https://friday.ai/signals/subscribe"

	return msg
}

func (me *MarketingEngine) GetStats() map[string]any {
	me.mu.RLock()
	defer me.mu.RUnlock()

	active := 0
	for _, c := range me.discoveredCommunities {
		if c.Active {
			active++
		}
	}

	return map[string]any{
		"communities_found":      len(me.discoveredCommunities),
		"communities_active":     active,
		"free_signals_posted":    me.freeSignalsPosted,
		"paid_conversions":       me.paidConversions,
		"last_outreach":          me.lastOutreach.Format(time.RFC3339),
		"outreach_interval_hours": me.outreachInterval.Hours(),
	}
}

var _ = math.Abs