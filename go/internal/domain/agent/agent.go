package agent

import (
	"context"
	"time"

	"github.com/friday-prototype/friday-go/pkg/util"
)

// AgentType represents the type of agent
type AgentType string

const (
	AgentTypeRouter      AgentType = "router"
	AgentTypePlanner     AgentType = "planner"
	AgentTypeResearcher  AgentType = "researcher"
	AgentTypeCoder       AgentType = "coder"
	AgentTypeJudge       AgentType = "judge"
	AgentTypeVerifier    AgentType = "verifier"
	AgentTypeBuilder     AgentType = "builder"
	AgentTypeReasoner    AgentType = "reasoner"
	AgentTypeCompanion   AgentType = "companion"
)

// AgentStatus represents the current status of an agent
type AgentStatus string

const (
	AgentStatusIdle     AgentStatus = "idle"
	AgentStatusRunning  AgentStatus = "running"
	AgentStatusWaiting  AgentStatus = "waiting"
	AgentStatusComplete AgentStatus = "complete"
	AgentStatusFailed   AgentStatus = "failed"
)

// Agent represents an autonomous worker
type Agent struct {
	ID          string
	Name        string
	Type        AgentType
	Description string
	Capabilities []Capability
	Tools       []string
	Status      AgentStatus
	Config      map[string]any
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func NewAgent(name string, typ AgentType, description string) *Agent {
	return &Agent{
		ID:          util.GenerateIDWithPrefix("agent"),
		Name:        name,
		Type:        typ,
		Description: description,
		Capabilities: []Capability{},
		Tools:       []string{},
		Status:      AgentStatusIdle,
		Config:      make(map[string]any),
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
}

func (a *Agent) WithCapability(cap Capability) *Agent {
	a.Capabilities = append(a.Capabilities, cap)
	return a
}

func (a *Agent) WithTool(tool string) *Agent {
	a.Tools = append(a.Tools, tool)
	return a
}

func (a *Agent) WithConfig(key string, value any) *Agent {
	a.Config[key] = value
	return a
}

func (a *Agent) CanHandle(taskType string) bool {
	for _, cap := range a.Capabilities {
		if cap.Type == taskType {
			return true
		}
	}
	return false
}

// Capability represents what an agent can do
type Capability struct {
	Type        string
	Description string
	Confidence  float64 // 0-1
}

// Task represents a unit of work for an agent
type Task struct {
	ID          string
	Type        string
	Description string
	Input       map[string]any
	Context     map[string]any
	Priority    int
	AssignedTo  string
	Status      TaskStatus
	Result      *TaskResult
	Error       string
	CreatedAt   time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
	Timeout     time.Duration
	Retries     int
	MaxRetries  int
}

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusAssigned  TaskStatus = "assigned"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusComplete  TaskStatus = "complete"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled"
)

type TaskResult struct {
	Output    any
	Artifacts []Artifact
	Metrics   map[string]any
}

type Artifact struct {
	Name     string
	Type     string
	Content  []byte
	Metadata map[string]any
}

func NewTask(taskType, description string, input map[string]any) *Task {
	return &Task{
		ID:          util.GenerateIDWithPrefix("task"),
		Type:        taskType,
		Description: description,
		Input:       input,
		Context:     make(map[string]any),
		Priority:    5,
		Status:      TaskStatusPending,
		MaxRetries:  3,
		Timeout:     5 * time.Minute,
		CreatedAt:   time.Now().UTC(),
	}
}

// Handoff represents passing work between agents
type Handoff struct {
	FromAgent   string
	ToAgent     string
	TaskID      string
	Reason      string
	Context     map[string]any
	CreatedAt   time.Time
}

// AgentInterface defines the contract for agent implementations
type AgentInterface interface {
	ID() string
	Name() string
	Type() AgentType
	Capabilities() []Capability
	Execute(ctx context.Context, task *Task) (*TaskResult, error)
	CanHandle(task *Task) bool
	HealthCheck(ctx context.Context) error
}

// Coordinator orchestrates multiple agents
type Coordinator interface {
	RegisterAgent(agent AgentInterface) error
	UnregisterAgent(agentID string) error
	Delegate(ctx context.Context, task *Task) (*TaskResult, error)
	DelegateWithHandoff(ctx context.Context, task *Task, fromAgent string) (*TaskResult, error)
	GetAgent(agentID string) (AgentInterface, error)
	ListAgents() []AgentInterface
	Shutdown(ctx context.Context) error
}