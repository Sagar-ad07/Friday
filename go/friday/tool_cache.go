package friday

import (
	"container/list"
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"
)

type cacheEntry struct {
	data      any
	expiresAt time.Time
	element   *list.Element
}

type toolStats struct {
	hits      int64
	misses    int64
	recentHit bool
}

type circuitBreaker struct {
	failures   int
	lastFail   time.Time
	openUntil  time.Time
	halfOpen   bool
}

var (
	cacheMap    = make(map[string]cacheEntry)
	cacheList   = list.New()
	cacheMu     sync.RWMutex
	maxCache    = 500
	toolStatsMap = make(map[string]*toolStats)
	breakerMap  = make(map[string]*circuitBreaker)
	statsMu     sync.RWMutex
)

var cacheableTools = map[string]time.Duration{
	"web_search":        60 * time.Second,
	"crypto_price":      30 * time.Second,
	"market_regime":     60 * time.Second,
	"get_account_info":  10 * time.Second,
	"get_positions":      5 * time.Second,
	"get_orders":         5 * time.Second,
	"trading_status":     5 * time.Second,
	"system_info":       10 * time.Second,
	"weather":          300 * time.Second,
	"wikipedia":        600 * time.Second,
	"semantic_recall":   30 * time.Second,
	"fts5_recall":       15 * time.Second,
}

func cacheKey(toolName string, args json.RawMessage) string {
	return toolName + ":" + string(args)
}

func getBreaker(toolName string) *circuitBreaker {
	statsMu.Lock()
	defer statsMu.Unlock()
	b, ok := breakerMap[toolName]
	if !ok {
		b = &circuitBreaker{}
		breakerMap[toolName] = b
	}
	return b
}

func getToolStats(toolName string) *toolStats {
	statsMu.Lock()
	defer statsMu.Unlock()
	s, ok := toolStatsMap[toolName]
	if !ok {
		s = &toolStats{}
		toolStatsMap[toolName] = s
	}
	return s
}

func isCircuitOpen(toolName string) bool {
	b := getBreaker(toolName)
	if b.openUntil.IsZero() {
		return false
	}
	if time.Now().After(b.openUntil) {
		b.openUntil = time.Time{}
		b.halfOpen = true
		return false
	}
	return true
}

func recordFailure(toolName string) {
	b := getBreaker(toolName)
	b.failures++
	b.lastFail = time.Now()
	if b.failures >= 3 {
		backoff := 30 * time.Second
		if b.failures > 5 {
			backoff = 60 * time.Second
		}
		if b.failures > 10 {
			backoff = 120 * time.Second
		}
		b.openUntil = time.Now().Add(backoff)
		b.halfOpen = false
		log.Printf("[CACHE] circuit breaker opened for %s (%d failures, backoff %v)", toolName, b.failures, backoff)
	}
}

func recordSuccess(toolName string) {
	b := getBreaker(toolName)
	b.failures = 0
	b.openUntil = time.Time{}
	b.halfOpen = false
}

func adaptiveTTL(toolName string) time.Duration {
	base := cacheableTools[toolName]
	s := getToolStats(toolName)
	if s.hits+s.misses == 0 {
		return base
	}
	ratio := float64(s.hits) / float64(s.hits+s.misses)
	if ratio > 0.8 && s.hits > 5 {
		return base * 2
	}
	if ratio < 0.2 && s.misses > 5 {
		return base / 2
	}
	return base
}

func getCachedResult(toolName string, args json.RawMessage) (any, bool) {
	ttl, ok := cacheableTools[toolName]
	if !ok {
		return nil, false
	}
	_ = ttl

	s := getToolStats(toolName)
	key := cacheKey(toolName, args)

	cacheMu.RLock()
	entry, exists := cacheMap[key]
	cacheMu.RUnlock()

	if !exists {
		statsMu.Lock()
		s.misses++
		statsMu.Unlock()
		return nil, false
	}
	if time.Now().After(entry.expiresAt) {
		cacheMu.Lock()
		delete(cacheMap, key)
		if entry.element != nil {
			cacheList.Remove(entry.element)
		}
		cacheMu.Unlock()
		statsMu.Lock()
		s.misses++
		statsMu.Unlock()
		return nil, false
	}

	cacheMu.Lock()
	if entry.element != nil {
		cacheList.MoveToFront(entry.element)
	}
	cacheMu.Unlock()

	statsMu.Lock()
	s.hits++
	s.recentHit = true
	statsMu.Unlock()

	return entry.data, true
}

func setCachedResult(toolName string, args json.RawMessage, data any) {
	_, ok := cacheableTools[toolName]
	if !ok {
		return
	}

	key := cacheKey(toolName, args)
	ttl := adaptiveTTL(toolName)

	cacheMu.Lock()
	defer cacheMu.Unlock()

	if existing, ok := cacheMap[key]; ok {
		existing.data = data
		existing.expiresAt = time.Now().Add(ttl)
		if existing.element != nil {
			cacheList.MoveToFront(existing.element)
		}
		cacheMap[key] = existing
		return
	}

	if len(cacheMap) >= maxCache {
		back := cacheList.Back()
		if back != nil {
			evictKey := back.Value.(string)
			delete(cacheMap, evictKey)
			cacheList.Remove(back)
		}
	}

	elem := cacheList.PushFront(key)
	cacheMap[key] = cacheEntry{
		data:      data,
		expiresAt: time.Now().Add(ttl),
		element:   elem,
	}
}

func CachedExecute(ctx context.Context, registry *ToolRegistry, name string, args json.RawMessage) (any, error) {
	if isCircuitOpen(name) {
		return nil, &circuitOpenError{tool: name}
	}

	if data, ok := getCachedResult(name, args); ok {
		return data, nil
	}

	result, err := registry.Execute(ctx, name, args)
	if err != nil {
		recordFailure(name)
		return nil, err
	}

	recordSuccess(name)
	setCachedResult(name, args, result)
	return result, nil
}

type circuitOpenError struct {
	tool string
}

func (e *circuitOpenError) Error() string {
	return "tool " + e.tool + " is circuit-breaker open (too many failures). Try again later."
}

func CacheStats() map[string]any {
	statsMu.RLock()
	defer statsMu.RUnlock()

	toolStats := make(map[string]any)
	for name, s := range toolStatsMap {
		total := s.hits + s.misses
		ratio := 0.0
		if total > 0 {
			ratio = float64(s.hits) / float64(total)
		}
		b := breakerMap[name]
		open := b != nil && !b.openUntil.IsZero()
		toolStats[name] = map[string]any{
			"hits":           s.hits,
			"misses":         s.misses,
			"hit_ratio":      ratio,
			"circuit_open":   open,
		}
	}

	cacheMu.RLock()
	size := len(cacheMap)
	cacheMu.RUnlock()

	return map[string]any{
		"size":      size,
		"max_size":  maxCache,
		"tool_stats": toolStats,
	}
}