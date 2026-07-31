package friday

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"
)

// ============================================================================
// CORE TYPES
// ============================================================================

type Role string

type Provider string

const (
	ProviderGLM        Provider = "zhipu"
	ProviderOpenRouter Provider = "openrouter"
	ProviderGemini     Provider = "gemini"
	ProviderOpencode   Provider = "opencode"
	ProviderOllama     Provider = "ollama"
	ProviderGroq       Provider = "groq"
	ProviderNvidia     Provider = "nvidia"
	ProviderGithub     Provider = "github"
	ProviderQwen       Provider = "qwen"
)

type Candidate struct {
	Provider Provider `json:"provider"`
	Model    string   `json:"model"`
}

type Chain []Candidate

// Message represents a chat message
type Message struct {
	Role         string          `json:"role"`
	Content      string          `json:"content"`
	Name         string          `json:"name,omitempty"`
	ToolCallID   string          `json:"tool_call_id,omitempty"`
	ToolCalls    []ToolCall      `json:"tool_calls,omitempty"`
	FunctionCall *FunctionCall   `json:"function_call,omitempty"`
}

type FunctionCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// ToolDef is the OpenAI "tools" field entry: {type:"function", function:{name, description, parameters}}.
// ToolSchema already matches the OpenAI "parameters" shape ({type, properties, required}).
type ToolDef struct {
	Type     string     `json:"type"` // always "function"
	Function ToolDefFn  `json:"function"`
}

type ToolDefFn struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Parameters  ToolSchema `json:"parameters"`
}

// ChatRequest for LLM Bridge
type ChatRequest struct {
	Model            string    `json:"model"`
	Messages         []Message `json:"messages"`
	Temperature      *float64  `json:"temperature,omitempty"`
	MaxTokens        *int      `json:"max_tokens,omitempty"`
	TopP             *float64  `json:"top_p,omitempty"`
	Stream           bool      `json:"stream,omitempty"`
	Stop             []string  `json:"stop,omitempty"`
	ResponseFormat   *struct{ Type string `json:"type"` } `json:"response_format,omitempty"`
	Tools            []ToolDef `json:"tools,omitempty"`
	ToolChoice       any       `json:"tool_choice,omitempty"` // "auto" | "none" | {"type":"function","function":{"name":"..."}}
}

// ChatCompletionResponse from LLM Bridge
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamEvent for SSE streaming
type StreamEvent struct {
	Type    string      `json:"type"`
	Content string      `json:"-"`
	Action  *ToolCall   `json:"-"`
	Result  interface{} `json:"result,omitempty"`
	Error   string      `json:"error,omitempty"`
	Done    bool        `json:"done,omitempty"`
	RunID   string      `json:"run_id,omitempty"`
	Worker  string      `json:"-"`
}

// MarshalJSON maps internal field names to what app.js expects per event type
func (e StreamEvent) MarshalJSON() ([]byte, error) {
	m := map[string]interface{}{"type": e.Type}
	if e.RunID != "" {
		m["run_id"] = e.RunID
	}
	switch e.Type {
	case "thought":
		m["name"] = e.Worker
		m["thought"] = e.Content
	case "action":
		m["name"] = e.Worker
		if e.Action != nil {
			m["action"] = e.Action.Function.Name
		}
	case "result":
		m["name"] = e.Worker
		m["result"] = e.Content
		if e.Action != nil {
			m["action"] = e.Action.Function.Name
		}
	case "final":
		m["name"] = e.Worker
		m["reply"] = e.Content
		m["done"] = e.Done
	case "error":
		m["error"] = e.Error
	case "worker_status":
		m["status"] = e.Content
		m["worker"] = e.Worker
	case "audio":
		m["audio"] = e.Content
	}
	return json.Marshal(m)
}



// ModelInfo from /v1/models
type ModelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type ModelsResponse struct {
	Object string      `json:"object"`
	Data   []ModelInfo `json:"data"`
}

// ============================================================================
// ERROR TYPES
// ============================================================================

type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
	Param   string `json:"param,omitempty"`
}

// ============================================================================
// HEALTH & STATS
// ============================================================================

type HealthResponse struct {
	Status      string            `json:"status"`
	Time        int64             `json:"time"`
	Version     string            `json:"version"`
	ServerAlive bool              `json:"server_alive"`
	Errors      []string          `json:"errors"`
	Providers   ProvidersStatus   `json:"providers"`
	Stats       ServerStats       `json:"stats"`
}

type ProvidersStatus struct {
	GLM    ProviderStatus `json:"glm"`
	Direct ProviderStatus `json:"direct"`
}

type ProviderStatus struct {
	Available   bool   `json:"available"`
	CircuitOpen bool   `json:"circuit_open"`
	Failures    int    `json:"failures"`
	LatencyMs   int64  `json:"latency_ms"`
}

type ServerStats struct {
	TotalRequests      int64 `json:"total_requests"`
	SuccessfulRequests int64 `json:"successful_requests"`
	FailedRequests     int64 `json:"failed_requests"`
	CacheHits          int64 `json:"cache_hits"`
	CacheMisses        int64 `json:"cache_misses"`
	ActiveConnections  int32 `json:"active_connections"`
	CacheSize          int   `json:"cache_size"`
	ConversationsCount int   `json:"conversations"`
}

// ============================================================================
// TOOLS
// ============================================================================

type ToolCallRequest struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ToolResult struct {
	Tool   string      `json:"tool"`
	Result interface{} `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
}

// ============================================================================
// UPGRADER TYPES
// ============================================================================

type UpgradeProposal struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Files       map[string]string `json:"files"`
	Tests       string            `json:"tests"`
	Status      string            `json:"status"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	AppliedAt   *time.Time        `json:"applied_at,omitempty"`
	RollbackAt  *time.Time        `json:"rollback_at,omitempty"`
	Error       string            `json:"error,omitempty"`
	TestOutput  string            `json:"test_output,omitempty"`
	Checksum    string            `json:"checksum"`
}

const (
	StatusPlanned     = "planned"
	StatusBuilt       = "built"
	StatusTestedPass  = "tested_pass"
	StatusTestedFail  = "tested_fail"
	StatusApproved    = "approved"
	StatusApplied     = "applied"
	StatusRejected    = "rejected"
	StatusRolledBack  = "rolled_back"
	StatusError       = "error"
)

type UpgradeLedger struct {
	mu        sync.RWMutex
	proposals map[string]*UpgradeProposal
	path      string
}

func (l *UpgradeLedger) Load() {
	l.mu.Lock()
	defer l.mu.Unlock()

	data, err := os.ReadFile(l.path)
	if err != nil {
		l.proposals = make(map[string]*UpgradeProposal)
		return
	}
	if err := json.Unmarshal(data, &l.proposals); err != nil {
		l.proposals = make(map[string]*UpgradeProposal)
	}
	if l.proposals == nil {
		l.proposals = make(map[string]*UpgradeProposal)
	}
}

func (l *UpgradeLedger) Save(p *UpgradeProposal) {
	l.mu.Lock()
	defer l.mu.Unlock()

	p.UpdatedAt = time.Now()
	l.proposals[p.ID] = p
	l.persist()
}

func (l *UpgradeLedger) persist() {
	data, err := json.MarshalIndent(l.proposals, "", "  ")
	if err != nil {
		log.Printf("UpgradeLedger: marshal error: %v", err)
		return
	}
	if err := os.WriteFile(l.path, data, 0644); err != nil {
		log.Printf("UpgradeLedger: write error: %v", err)
	}
}

func (l *UpgradeLedger) Get(id string) (*UpgradeProposal, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	p, ok := l.proposals[id]
	return p, ok
}

func (l *UpgradeLedger) List() []*UpgradeProposal {
	l.mu.RLock()
	defer l.mu.RUnlock()
	result := make([]*UpgradeProposal, 0, len(l.proposals))
	for _, p := range l.proposals {
		result = append(result, p)
	}
	return result
}

func (p *UpgradeProposal) computeChecksum() string {
	data := p.ID + p.Title + p.Description
	for k, v := range p.Files {
		data += k + v
	}
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])[:16]
}

// ============================================================================
// SEMANTIC CACHE (for LLM client)
// ============================================================================

type CacheEntry struct {
	Response   *ChatCompletionResponse
	Provider   string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	HitCount   int64
}

type SemanticCache struct {
	mu       sync.RWMutex
	entries  map[string]*CacheEntry
	capacity int
	ttl      time.Duration
	hits     int64
	misses   int64
}

func NewSemanticCache(capacity int, ttl time.Duration) *SemanticCache {
	return &SemanticCache{
		entries:  make(map[string]*CacheEntry),
		capacity: capacity,
		ttl:      ttl,
	}
}

func (sc *SemanticCache) Key(req *ChatRequest) string {
	data := fmt.Sprintf("%s:%v:%v:%v:%v", req.Model, req.Messages, req.Temperature, req.MaxTokens, req.TopP)
	h := fnv.New64a()
	h.Write([]byte(data))
	return fmt.Sprintf("%x", h.Sum64())
}

func (sc *SemanticCache) Get(key string) (*ChatCompletionResponse, string, bool) {
	sc.mu.RLock()
	entry, ok := sc.entries[key]
	sc.mu.RUnlock()

	if !ok {
		return nil, "", false
	}
	if time.Now().After(entry.ExpiresAt) {
		sc.mu.Lock()
		delete(sc.entries, key)
		sc.mu.Unlock()
		return nil, "", false
	}
	return entry.Response, entry.Provider, true
}

func (sc *SemanticCache) Set(key string, resp *ChatCompletionResponse, provider string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if len(sc.entries) >= sc.capacity {
		sc.evictOldest()
	}

	sc.entries[key] = &CacheEntry{
		Response:  resp,
		Provider:  provider,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(sc.ttl),
	}
}

func (sc *SemanticCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time
	for k, v := range sc.entries {
		if oldestTime.IsZero() || v.CreatedAt.Before(oldestTime) {
			oldestKey, oldestTime = k, v.CreatedAt
		}
	}
	if oldestKey != "" {
		delete(sc.entries, oldestKey)
	}
}

func (sc *SemanticCache) Stats() (int, int64, int64) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return len(sc.entries), sc.hits, sc.misses
}

// ============================================================================
// CONVERSATION STORE
// ============================================================================

type Conversation struct {
	ID        string
	Messages  []Message
	CreatedAt time.Time
	UpdatedAt time.Time
}

type ConversationStore struct {
	mu            sync.RWMutex
	conversations map[string]*Conversation
	maxSize       int
}

func NewConversationStore(maxSize int) *ConversationStore {
	return &ConversationStore{
		conversations: make(map[string]*Conversation),
		maxSize:       maxSize,
	}
}

func (cs *ConversationStore) Get(id string) (*Conversation, bool) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	conv, ok := cs.conversations[id]
	return conv, ok
}

func (cs *ConversationStore) Append(id string, msg Message) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	conv, ok := cs.conversations[id]
	if !ok {
		if len(cs.conversations) >= cs.maxSize {
			cs.evictOldest()
		}
		conv = &Conversation{
			ID:        id,
			Messages:  []Message{},
			CreatedAt: time.Now(),
		}
		cs.conversations[id] = conv
	}
	conv.Messages = append(conv.Messages, msg)
	conv.UpdatedAt = time.Now()
}

func (cs *ConversationStore) evictOldest() {
	var oldestID string
	var oldestTime time.Time
	for id, conv := range cs.conversations {
		if oldestTime.IsZero() || conv.UpdatedAt.Before(oldestTime) {
			oldestID, oldestTime = id, conv.UpdatedAt
		}
	}
	if oldestID != "" {
		delete(cs.conversations, oldestID)
	}
}

func (cs *ConversationStore) Count() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return len(cs.conversations)
}

// ============================================================================
// SHARED TYPES FOR ALPHA ENGINE
// ============================================================================

// AlphaCampaign represents a campaign with Friday's alpha metrics
type AlphaCampaign struct {
	ID              string                 `json:"id"`
	Chain           string                 `json:"chain"`
	Name            string                 `json:"name"`
	Status          string                 `json:"status"` // hunting, positioning, farming, claiming, done

	// Alpha metrics (Friday's proprietary)
	PredictedEV     float64                `json:"predicted_ev_usd"`
	EVConfidence    float64                `json:"ev_confidence"`
	AlphaScore      float64                `json:"alpha_score"`      // 0-1, how much edge we have
	CompetitionLevel float64               `json:"competition_level"` // 0-1, how crowded
	SybilResistance float64                `json:"sybil_resistance"` // 0-1, how hard to Sybil
	GameTheoryEV    float64                `json:"game_theory_ev"`   // Nash equilibrium EV

	// Strategy
	ActiveStrategy  *EvolvedStrategy       `json:"active_strategy"`
	BackupStrategies []*EvolvedStrategy    `json:"backup_strategies"`
	CapitalAllocated float64               `json:"capital_allocated"`

	// Intelligence
	CompetitorCount int                    `json:"competitor_count"`
	CompetitorProfiles []*CompetitorProfile `json:"-"`
	AlphaSignals    []AlphaSignal          `json:"alpha_signals"`

	// Timing
	DetectedAt      time.Time              `json:"detected_at"`
	PositionedAt    time.Time              `json:"positioned_at"`
	ClaimDeadline   time.Time              `json:"claim_deadline"`

	// Risk
	MaxDrawdown     float64                `json:"max_drawdown"`
	RiskScore       float64                `json:"risk_score"`

	// Auto-config
	AutoDeploy      bool                   `json:"auto_deploy"`
	AutoTransact    bool                   `json:"auto_transact"`
	AutoClaim       bool                   `json:"auto_claim"`

	UpdatedAt       time.Time              `json:"updated_at"`
}

type AlphaSignal struct {
	Source             string                 `json:"source"`            // onchain, mempool, social, git, competitor
	Type               string                 `json:"type"`              // deployment, whale_accumulation, sentiment_shift, competitor_move
	Strength           float64                `json:"strength"`          // 0-1
	AlphaContribution  float64                `json:"alpha_contribution"` // how much EV this signal adds
	Timestamp          time.Time              `json:"timestamp"`
	Metadata           map[string]string      `json:"metadata"`
}

// CampaignOutcome records the result of a completed campaign
type CampaignOutcome struct {
	CampaignID    string    `json:"campaign_id"`
	Chain         string    `json:"chain"`
	TeamPedigree  float64   `json:"team_pedigree"`   // 0-1
	InvestorTier  float64   `json:"investor_tier"`   // 0-1
	Tokenomics    float64   `json:"tokenomics"`      // 0-1
	Duration      int       `json:"duration_days"`
	TVLAtLaunch   float64   `json:"tvl_at_launch"`
	ActualReward  float64   `json:"actual_reward_usd"`
	StrategyUsed  string    `json:"strategy_used"`
	Timestamp     time.Time `json:"timestamp"`
}


type EvolvedStrategy struct{}
type CompetitorProfile struct{}
type CapitalAllocation struct{}
type GameTheoryOptimizer struct{}
type CompetitorTracker struct{}
type DynamicCapitalAllocator struct{}
type StrategyEvolver struct{}
type RiskManager struct{}

// ============================================================================
// UTILITY FUNCTIONS
// ============================================================================

func hashString(s string) string {
	h := 0
	for i := 0; i < len(s); i++ {
		h = 31*h + int(s[i])
	}
	if h < 0 {
		h = -h
	}
	return fmt.Sprintf("%x", h)
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = "abcdefghijklmnopqrstuvwxyz0123456789"[rand.Intn(len("abcdefghijklmnopqrstuvwxyz0123456789"))]
	}
	return string(b)
}
