package memory

import (
	"context"
	"time"

	"github.com/friday-prototype/friday-go/pkg/util"
)

// MemoryType represents the type of memory
type MemoryType string

const (
	MemoryTypeEpisodic  MemoryType = "episodic"  // Conversation history
	MemoryTypeSemantic  MemoryType = "semantic"  // Facts and knowledge
	MemoryTypeProcedural MemoryType = "procedural" // Skills and procedures
)

// Memory represents a single memory entry
type Memory struct {
	ID          string
	Type        MemoryType
	Content     string
	Summary     string
	Tags        []string
	Embedding   []float64
	Metadata    map[string]any
	Source      string
	Confidence  float64
	Importance  float64 // 0-1
	AccessCount int
	CreatedAt   time.Time
	UpdatedAt   time.Time
	AccessedAt  time.Time
	ExpiresAt   *time.Time
}

func NewMemory(memType MemoryType, content string) *Memory {
	now := time.Now().UTC()
	return &Memory{
		ID:          util.GenerateIDWithPrefix("mem"),
		Type:        memType,
		Content:     content,
		Tags:        []string{},
		Metadata:    make(map[string]any),
		Confidence:  1.0,
		Importance:  0.5,
		CreatedAt:   now,
		UpdatedAt:   now,
		AccessedAt:  now,
	}
}

// Episode represents a conversation episode
type Episode struct {
	ID          string
	SessionID   string
	Title       string
	Summary     string
	Messages    []MessageRef
	StartTime   time.Time
	EndTime     time.Time
	Metadata    map[string]any
	Tags        []string
	Importance  float64
}

type MessageRef struct {
	Role      string
	Content   string
	Timestamp time.Time
	TokenCount int
}

func NewEpisode(sessionID string) *Episode {
	now := time.Now().UTC()
	return &Episode{
		ID:        util.GenerateIDWithPrefix("ep"),
		SessionID: sessionID,
		StartTime: now,
		EndTime:   now,
		Messages:  []MessageRef{},
		Metadata:  make(map[string]any),
		Tags:      []string{},
		Importance: 0.5,
	}
}

// Fact represents an extracted fact
type Fact struct {
	ID         string
	Subject    string
	Predicate  string
	Object     string
	Confidence float64
	Source     string
	Evidence   []string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	ExpiresAt  *time.Time
}

func NewFact(subject, predicate, object string, confidence float64) *Fact {
	now := time.Now().UTC()
	return &Fact{
		ID:         util.GenerateIDWithPrefix("fact"),
		Subject:    subject,
		Predicate:  predicate,
		Object:     object,
		Confidence: confidence,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

// SearchQuery represents a memory search query
type SearchQuery struct {
	Query       string
	QueryVector []float64
	Types       []MemoryType
	Tags        []string
	Limit       int
	Threshold   float64
	TimeRange   *TimeRange
	Filters     map[string]any
}

type TimeRange struct {
	Start time.Time
	End   time.Time
}

// SearchResult represents a search result
type SearchResult struct {
	Memory   *Memory
	Score    float64
	Distance float64
}

// ConsolidationJob represents a memory consolidation task
type ConsolidationJob struct {
	ID        string
	Type      string
	Status    string
	Input     []string
	Output    []string
	Error     string
	CreatedAt time.Time
	StartedAt *time.Time
	CompletedAt *time.Time
}

// MemoryInterface defines the contract for memory storage
type MemoryInterface interface {
	Store(ctx context.Context, mem *Memory) error
	Get(ctx context.Context, id string) (*Memory, error)
	Delete(ctx context.Context, id string) error
	Search(ctx context.Context, query *SearchQuery) ([]*SearchResult, error)
	Update(ctx context.Context, mem *Memory) error
	List(ctx context.Context, memType MemoryType, limit int, offset int) ([]*Memory, error)
	Count(ctx context.Context, memType MemoryType) (int64, error)
	Stats() MemoryStats
}

type MemoryStats struct {
	TotalMemories   int64
	EpisodicCount   int64
	SemanticCount   int64
	ProceduralCount int64
	TotalVectors    int64
	IndexSize       int64
}

// EpisodicInterface defines the contract for episodic memory
type EpisodicInterface interface {
	StartEpisode(ctx context.Context, sessionID string) (*Episode, error)
	EndEpisode(ctx context.Context, episodeID string) error
	AddMessage(ctx context.Context, episodeID string, role, content string, tokens int) error
	GetEpisode(ctx context.Context, episodeID string) (*Episode, error)
	GetEpisodes(ctx context.Context, sessionID string, limit int) ([]*Episode, error)
	SearchEpisodes(ctx context.Context, query string, limit int) ([]*Episode, error)
}

// SemanticInterface defines the contract for semantic memory
type SemanticInterface interface {
	StoreFact(ctx context.Context, fact *Fact) error
	GetFact(ctx context.Context, id string) (*Fact, error)
	QueryFacts(ctx context.Context, subject, predicate string) ([]*Fact, error)
	ExtractFacts(ctx context.Context, text string) ([]*Fact, error)
	MergeFacts(ctx context.Context, facts []*Fact) ([]*Fact, error)
}

// ConsolidationInterface defines the contract for memory consolidation
type ConsolidationInterface interface {
	ConsolidateEpisodic(ctx context.Context, sessionID string) (*ConsolidationJob, error)
	ConsolidateSemantic(ctx context.Context) (*ConsolidationJob, error)
	GetJob(ctx context.Context, jobID string) (*ConsolidationJob, error)
	ListJobs(ctx context.Context, status string) ([]*ConsolidationJob, error)
}