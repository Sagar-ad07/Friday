package llm

// Role represents the role of a message in a conversation
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
	RoleFunction  Role = "function"
)

// Message represents a single message in a conversation
type Message struct {
	Role       Role
	Content    string
	Name       string         // For tool/function calls
	ToolCalls  []ToolCall     // Assistant tool calls
	ToolCallID string         // For tool responses
	Metadata   map[string]any // Extra data (tokens, timing, etc.)
}

func NewSystemMessage(content string) *Message {
	return &Message{Role: RoleSystem, Content: content}
}

func NewUserMessage(content string) *Message {
	return &Message{Role: RoleUser, Content: content}
}

func NewAssistantMessage(content string) *Message {
	return &Message{Role: RoleAssistant, Content: content}
}

func NewToolMessage(toolCallID, content string) *Message {
	return &Message{Role: RoleTool, Content: content, ToolCallID: toolCallID}
}

func (m *Message) WithMetadata(key string, value any) *Message {
	if m.Metadata == nil {
		m.Metadata = make(map[string]any)
	}
	m.Metadata[key] = value
	return m
}

func (m *Message) GetMetadata(key string) (any, bool) {
	if m.Metadata == nil {
		return nil, false
	}
	v, ok := m.Metadata[key]
	return v, ok
}