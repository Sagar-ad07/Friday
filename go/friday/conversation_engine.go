package friday

import (
	"sync"
	"time"
)

// ConversationEngine manages active conversations
type ConversationEngine struct {
	activeConversations map[string]*ActiveConversation
	mu sync.RWMutex
}

type ActiveConversation struct {
	userID string
	turnCount int
	lastActivity time.Time
	topic string
}

// NewConversationEngine creates a new conversation engine
func NewConversationEngine() *ConversationEngine {
	return &ConversationEngine{
		activeConversations: make(map[string]*ActiveConversation),
	}
}

// StartConversation starts a new conversation
func (ce *ConversationEngine) StartConversation(userID, topic string) {
	ce.mu.Lock()
	defer ce.mu.Unlock()

	ce.activeConversations[userID] = &ActiveConversation{
		userID:        userID,
		turnCount:     0,
		lastActivity:  time.Now(),
		topic:         topic,
	}
}

// GetActiveConversation retrieves an active conversation
func (ce *ConversationEngine) GetActiveConversation(userID string) *ActiveConversation {
	ce.mu.RLock()
	defer ce.mu.RUnlock()

	if conv, exists := ce.activeConversations[userID]; exists {
		return conv
	}
	return nil
}

// EndConversation ends a conversation
func (ce *ConversationEngine) EndConversation(userID string) {
	ce.mu.Lock()
	defer ce.mu.Unlock()

	delete(ce.activeConversations, userID)
}

// UpdateActivity updates the last activity time
func (ce *ConversationEngine) UpdateActivity(userID string) {
	ce.mu.Lock()
	defer ce.mu.Unlock()

	if conv, exists := ce.activeConversations[userID]; exists {
		conv.lastActivity = time.Now()
		conv.turnCount++
	}
}

// CleanupIdleConversations removes conversations older than threshold
func (ce *ConversationEngine) CleanupIdleConversations(threshold time.Duration) {
	ce.mu.Lock()
	defer ce.mu.Unlock()

	now := time.Now()
	for userID, conv := range ce.activeConversations {
		if now.Sub(conv.lastActivity) > threshold {
			delete(ce.activeConversations, userID)
		}
	}
}

// GetActiveConversationCount returns the number of active conversations
func (ce *ConversationEngine) GetActiveConversationCount() int {
	ce.mu.RLock()
	defer ce.mu.RUnlock()
	return len(ce.activeConversations)
}
