package friday

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// ActivityEvent is a single entry in Friday's live activity stream.
// Kinds: think | tool | worker | chat | trade | system | monitor
type ActivityEvent struct {
	ID     string    `json:"id"`
	TS     time.Time `json:"ts"`
	Kind   string    `json:"kind"`
	Label  string    `json:"label"`
	Detail string    `json:"detail"`
}

type activityHub struct {
	mu       sync.RWMutex
	buffer   []ActivityEvent
	max      int
	subs     map[chan ActivityEvent]struct{}
}

var (
	activityOnce sync.Once
	hub          *activityHub
)

// InitActivityHub returns the singleton activity hub (created once).
func InitActivityHub() *activityHub {
	activityOnce.Do(func() {
		hub = &activityHub{
			buffer: make([]ActivityEvent, 0, 300),
			max:    300,
			subs:   make(map[chan ActivityEvent]struct{}),
		}
		log.Printf("[ACTIVITY] live activity hub online (buffer=%d)", hub.max)
	})
	return hub
}

// PublishActivity appends an event to the ring buffer and broadcasts it
// to every connected subscriber (non-blocking). Safe to call from any
// goroutine — orchestrator loops, the trading monitor, chat handlers.
func PublishActivity(kind, label, detail string) {
	if hub == nil {
		return
	}
	ev := ActivityEvent{
		ID:     uuid.New().String()[:12],
		TS:     time.Now(),
		Kind:   kind,
		Label:  label,
		Detail: detail,
	}
	hub.mu.Lock()
	hub.buffer = append(hub.buffer, ev)
	if len(hub.buffer) > hub.max {
		hub.buffer = hub.buffer[len(hub.buffer)-hub.max:]
	}
	for ch := range hub.subs {
		select {
		case ch <- ev:
		default: // slow subscriber — skip, never block the hub
		}
	}
	hub.mu.Unlock()
}

// History returns the last n events (newest last).
func (h *activityHub) History(n int) []ActivityEvent {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if n <= 0 || n > len(h.buffer) {
		n = len(h.buffer)
	}
	out := make([]ActivityEvent, n)
	copy(out, h.buffer[len(h.buffer)-n:])
	return out
}

// Subscribe registers a subscriber channel. Returns the channel plus an
// unsubscribe func. The channel is buffered (64) so bursts don't block
// the publisher; slow consumers are dropped by PublishActivity.
func (h *activityHub) Subscribe() (<-chan ActivityEvent, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan ActivityEvent, 64)
	h.subs[ch] = struct{}{}
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		close(ch)
		h.mu.Unlock()
	}
}

// ──────────────────────────────────────────────────────────────────────
// HTTP surface
// ──────────────────────────────────────────────────────────────────────

// ActivityHistoryHandler returns recent activity events as JSON.
func (s *Server) ActivityHistoryHandler(c *gin.Context) {
	limit := 100
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	hub := InitActivityHub()
	hub.mu.RLock()
	buflen := len(hub.buffer)
	hub.mu.RUnlock()
	h := hub.History(limit)
	c.JSON(http.StatusOK, gin.H{"events": h, "count": len(h), "buffer_len": buflen})
}

// ActivityWSHandler streams activity events over a WebSocket so the
// control center shows Friday's live thinking, tool calls, workers and
// trading events with zero polling.
func (s *Server) ActivityWSHandler(c *gin.Context) {
	conn, err := wsUpgrade.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[ACTIVITY-WS] upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	hub := InitActivityHub()
	ch, unsub := hub.Subscribe()
	defer unsub()

	// Push recent history first so a fresh client renders instantly.
	hist := hub.History(50)
	for _, ev := range hist {
		payload, _ := json.Marshal(ev)
		if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			return
		}
	}

	// Ping keep-alive in the background.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		t := time.NewTicker(25 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			case <-stop:
				return
			}
		}
	}()

	for ev := range ch {
		payload, _ := json.Marshal(ev)
		if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			return
		}
	}
}

// truncateDetail keeps event payloads small for the wire + UI.
func truncateDetail(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
