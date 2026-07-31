package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Orchestrator struct {
	llm      *LLMClient
	registry *ToolRegistry
	mu       sync.RWMutex
	streams  map[string]chan StreamEvent
	cancels  map[string]context.CancelFunc
	config   *Config
}

func NewOrchestrator(cfg *Config, llm *LLMClient, registry *ToolRegistry) *Orchestrator {
	return &Orchestrator{
		llm:      llm,
		registry: registry,
		streams:  make(map[string]chan StreamEvent),
		cancels:  make(map[string]context.CancelFunc),
		config:   cfg,
	}
}

func (o *Orchestrator) AgenticRun(ctx context.Context, runID, text, lang, userID string) <-chan StreamEvent {
	ch := make(chan StreamEvent, 100)
	o.mu.Lock()
	o.streams[runID] = ch
	ctx, cancel := context.WithCancel(ctx)
	o.cancels[runID] = cancel
	o.mu.Unlock()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("CRASH RECOVERY: runFriday panicked: %v", r)
				ch <- StreamEvent{Type: "final", Content: "I encountered an error. Please try again.", RunID: runID, Done: true}
			}
			close(ch)
			o.mu.Lock()
			delete(o.streams, runID)
			delete(o.cancels, runID)
			o.mu.Unlock()
		}()
		o.runFriday(ctx, ch, runID, text)
	}()

	return ch
}

func (o *Orchestrator) handleLocal(ctx context.Context, text string, ch chan StreamEvent, runID string) string {
	t := strings.ToLower(text)

	switch {
	case HasTradingIntent(t) && !strings.Contains(t, "learn") && !strings.Contains(t, "teach"):
		snap := LiveState(ctx)
		msg := snap.LiveContextHuman()
		switch tradingIntentOf(t) {
		case "account":
			if snap.Account != nil {
				msg = fmt.Sprintf("Your %s account balance is %.2f %s (login %d). Equity: %.2f.",
					snap.Account.Server, snap.Account.Balance, snap.Account.Currency, snap.Account.Login, snap.Account.Equity)
			}
		case "equity":
			if snap.Account != nil {
				msg = fmt.Sprintf("Equity is %.2f %s on a balance of %.2f; margin in use %.2f.",
					snap.Account.Equity, snap.Account.Currency, snap.Account.Balance, snap.Account.Margin)
			}
		case "profit":
			if snap.Account != nil {
				msg = fmt.Sprintf("Floating profit right now: %+.2f %s (balance %.2f, equity %.2f).",
					snap.Account.Profit, snap.Account.Currency, snap.Account.Balance, snap.Account.Equity)
			}
		case "positions":
			if snap.Account == nil {
				msg = snap.LiveContextHuman()
			} else if len(snap.Positions) == 0 {
				msg = "No open positions right now."
			} else {
				var lines []string
				for _, p := range snap.Positions {
					lines = append(lines, fmt.Sprintf("%s %s %.2f lots @ %.5f → %.5f (%+.2f %s)",
						strings.ToUpper(p.Type), p.Symbol, p.Volume, p.PriceOpen, p.PriceCurrent, p.Profit, snap.Account.Currency))
				}
				msg = "Open positions:\n- " + strings.Join(lines, "\n- ")
			}
		}
		ch <- StreamEvent{Type: "final", Content: msg, RunID: runID, Done: true}
		return "local"

	case strings.Contains(t, "trading status") || strings.Contains(t, "bot status") || t == "status":
		snap := LiveState(ctx)
		head := fmt.Sprintf("Engine: %s", map[bool]string{true: "online", false: "UNREACHABLE"}[snap.EngineAlive])
		ch <- StreamEvent{Type: "final", Content: head + ".\n" + snap.LiveContextHuman(), RunID: runID, Done: true}
		return "local"

	case strings.Contains(t, "health") || strings.Contains(t, "alive"):
		up := "online"
		engine := "reachable"
		if !LiveState(ctx).EngineAlive {
			up = "online (LLM up)"
			engine = "unreachable"
		}
		ch <- StreamEvent{Type: "final", Content: fmt.Sprintf("I'm %s. Brain providers: %s. Trading engine: %s.", up, strings.Join(LLMProviderNames(), ", "), engine), RunID: runID, Done: true}
		return "local"

	case (t == "time" || t == "date" || t == "what time" || t == "whats the time" || t == "what date" || t == "whats the date" || strings.HasPrefix(t, "what time") || strings.HasPrefix(t, "what date")):
		ch <- StreamEvent{Type: "final", Content: time.Now().Format("Mon Jan 2 15:04:05 MST 2006"), RunID: runID, Done: true}
		return "local"

	case strings.Contains(t, "who are you") || strings.Contains(t, "what are you") || strings.Contains(t, "introduce"):
		ch <- StreamEvent{Type: "final", Content: "I'm Friday — your autonomous AI assistant. I manage trading, search, code, and files. All Go, all local.", RunID: runID, Done: true}
		return "local"

	case strings.Contains(t, "hello") || strings.Contains(t, "hi ") || t == "hi" || t == "hey":
		ch <- StreamEvent{Type: "final", Content: "Hey Boss. What do you need?", RunID: runID, Done: true}
		return "local"
	}

	return ""
}

func (o *Orchestrator) Cancel(runID string) bool {
	o.mu.RLock()
	cancel, ok := o.cancels[runID]
	o.mu.RUnlock()
	if ok {
		cancel()
		return true
	}
	return false
}

func (o *Orchestrator) CancelAll() {
	o.mu.RLock()
	for _, cancel := range o.cancels {
		cancel()
	}
	o.mu.RUnlock()
}

type ActionPlan struct {
	Thought string          `json:"thought"`
	Action  *ToolCall       `json:"action,omitempty"`
	Plan    []ToolCall      `json:"plan,omitempty"`
	Done    bool            `json:"done"`
	Answer  json.RawMessage `json:"answer,omitempty"`
}

func (a *ActionPlan) AnswerString() string {
	if len(a.Answer) == 0 { return "" }
	var s string
	if json.Unmarshal(a.Answer, &s) == nil { return s }
	// If it's not a string (e.g., an object), marshal it back
	b, _ := json.Marshal(a.Answer)
	if len(b) > 0 { return string(b) }
	return ""
}

func (o *Orchestrator) ChatHandler(c *gin.Context) {
	var req struct {
		Model    string          `json:"model"`
		Messages json.RawMessage `json:"messages"`
		Stream   bool            `json:"stream"`
		UserID   string          `json:"user_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	userID := req.UserID
	if userID == "" {
		userID = "default"
	}

	var msgs []Message
	json.Unmarshal(req.Messages, &msgs)
	lastUser := ""
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			lastUser = msgs[i].Content
			break
		}
	}

	runID := uuid.New().String()
	stream := o.AgenticRun(c.Request.Context(), runID, lastUser, "en", userID)

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	c.Stream(func(w io.Writer) bool {
		select {
		case event, ok := <-stream:
			if !ok {
				return false
			}
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", data)
			return true
		case <-c.Request.Context().Done():
			return false
		}
	})
}

func (o *Orchestrator) CommandHandler(c *gin.Context) {
	var req struct {
		Command string                 `json:"command"`
		Args    map[string]interface{} `json:"args"`
		UserID  string                 `json:"user_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	result, err := o.registry.Execute(ctx, req.Command, toJSONBytes(req.Args))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"result": result})
}

func (o *Orchestrator) CancelHandler(c *gin.Context) {
	runID := c.Param("id")
	ok := o.Cancel(runID)
	c.JSON(200, gin.H{"cancelled": ok})
}

func (o *Orchestrator) VoiceHandler(c *gin.Context) {
	c.JSON(200, gin.H{"status": "voice via LLM Bridge :9001"})
}

func toJSONBytes(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
