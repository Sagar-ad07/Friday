package friday

import (
	"strings"
	"sync"
	"time"
)

// ContextManager manages conversation context
type ContextManager struct {
	currentContext ConversationContext
	mu sync.RWMutex
}

type ConversationContext struct {
	UserPreferences map[string]string
	TurnHistory []ConversationTurn
	SpeakStyle SpeakStyle
	CurrentTopic string
}

type ConversationTurn struct {
	Timestamp time.Time
	UserText string
	AssistantText string
}

type SpeakStyle struct {
	Pacing string // slow, normal, fast
	EmotionalTone string // neutral, happy, serious, excited
}

// NewContextManager creates a new context manager
func NewContextManager() *ContextManager {
	return &ContextManager{
		currentContext: ConversationContext{
			UserPreferences: make(map[string]string),
			TurnHistory: make([]ConversationTurn, 0, 20),
			SpeakStyle: SpeakStyle{
				Pacing: "normal",
				EmotionalTone: "neutral",
			},
		},
	}
}

// GetCurrentContext returns the current context
func (cm *ContextManager) GetCurrentContext() ConversationContext {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.currentContext
}

// UpdateContext updates the conversation context
func (cm *ContextManager) UpdateContext(userText string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Add to turn history
	cm.currentContext.TurnHistory = append(cm.currentContext.TurnHistory, ConversationTurn{
		Timestamp: time.Now(),
		UserText: userText,
	})

	// Keep last 20 turns
	if len(cm.currentContext.TurnHistory) > 20 {
		cm.currentContext.TurnHistory = cm.currentContext.TurnHistory[1:]
	}

	// Update current topic
	cm.currentContext.CurrentTopic = extractTopic(userText)
}

// GetConversationStats returns conversation statistics
func (cm *ContextManager) GetConversationStats() map[string]interface{} {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return map[string]interface{}{
		"turn_count": len(cm.currentContext.TurnHistory),
		"current_topic": cm.currentContext.CurrentTopic,
		"total_conversations": len(cm.currentContext.TurnHistory),
	}
}

// extractTopic extracts the topic from text
func extractTopic(text string) string {
	topics := []string{"trading", "voice", "system", "earnings", "general"}
	textLower := strings.ToLower(text)

	for _, topic := range topics {
		if strings.Contains(textLower, topic) {
			return topic
		}
	}

	return "conversation"
}
