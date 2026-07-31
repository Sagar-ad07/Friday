package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────
// Cognitive Pipeline — replaces the flat tool-loop with staged
// perception → memory → routing → action → synthesis
// ──────────────────────────────────────────────────────────────────────

// PipelineContext carries per-turn state through the processing stages.
// Each stage reads from and writes to this struct.
type PipelineContext struct {
	UserText    string
	RunID       string
	Task        TaskType
	Messages    []Message
	SystemPrompt string
	ToolDefs    []ToolDef

	// Filled by stages
	InitialResponse *ChatCompletionResponse
	FinalAnswer     string
	CalledTools     []string
	StartTime       time.Time
}

// BuildSmartContext assembles the message list with four-tier memory:
//
//	Tier 1 — Last N messages verbatim (recency)
//	Tier 2 — Older messages summarized (compression)
//	Tier 3 — Decision memory oracle: past tool choices for similar queries
//	Tier 4 — Semantic recall from vector memory (precision)
//
// This is the "memory retrieval" phase of the cognitive pipeline.
func BuildSmartContext(text string, systemPrompt string) []Message {
	cs := GetCompanionState()
	history := cs.GetHistory()

	msgs := make([]Message, 0, len(history))
	for _, h := range history {
		msgs = append(msgs, Message{Role: h["role"], Content: h["content"]})
	}

	// Tier 1 + 2: standard context window management
	assembled := ManagedContext(msgs, systemPrompt)

	if !isMemoryWorthy(text) {
		return assembled
	}

	keyTerms := extractKeyTerms(text)

	// Tier 3: Decision Memory Oracle — past tool choices for similar queries.
	// This is the system learning from experience: "last time someone asked
	// about X, you needed tools Y and Z. Start there."
	dm := getDecisionMemory()
	if tools, confidence := dm.Suggest(text); len(tools) > 0 && confidence > 0.3 {
		hint := Message{
			Role:    "system",
			Content: fmt.Sprintf("[Oracle: past queries similar to this needed tools: %s (confidence %.0f%%)]", strings.Join(tools, ", "), confidence*100),
		}
		if len(assembled) > 0 && assembled[0].Role == "system" {
			tail := make([]Message, len(assembled)-1)
			copy(tail, assembled[1:])
			assembled = append([]Message{assembled[0], hint}, tail...)
		} else {
			assembled = append([]Message{hint}, assembled...)
		}
		log.Printf("[PIPELINE] oracle: %s (%.0f%%)", strings.Join(tools, ", "), confidence*100)
	}

	// Tier 4: semantic recall from vector memory.
	recall := RecallRelevantMemory(context.Background(), keyTerms, 3)
	if recall != "" {
		inject := Message{
			Role:    "system",
			Content: recall,
		}
		if len(assembled) > 0 && assembled[0].Role == "system" {
			tail := make([]Message, len(assembled)-1)
			copy(tail, assembled[1:])
			assembled = append([]Message{assembled[0], inject}, tail...)
		} else {
			assembled = append([]Message{inject}, assembled...)
		}
		log.Printf("[PIPELINE] injected %d semantic facts", strings.Count(recall, "\n- "))
	}

	return assembled
}

// isMemoryWorthy returns true if the input is substantive enough to
// warrant a semantic memory lookup. Filters out greetings, one-word
// pings, and time queries that don't need historical context.
func isMemoryWorthy(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if len(lower) < 10 {
		return false
	}
	// Skip greetings and trivial queries
	trivial := []string{"hello", "hi ", "hey", "what time", "what date",
		"who are you", "thank", "thanks", "ok", "okay", "good",
		"status", "health", "alive"}
	for _, t := range trivial {
		if strings.Contains(lower, t) {
			return false
		}
	}
	return true
}

// extractKeyTerms pulls the first N meaningful words for the FTS5 query.
// Strips stopwords, keeps nouns-like terms.
func extractKeyTerms(text string) string {
	stopwords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "can": true, "shall": true,
		"to": true, "of": true, "in": true, "for": true, "on": true,
		"with": true, "at": true, "by": true, "from": true, "as": true,
		"into": true, "through": true, "during": true, "before": true,
		"after": true, "above": true, "below": true, "between": true,
		"and": true, "but": true, "or": true, "nor": true, "not": true,
		"so": true, "yet": true, "both": true, "either": true, "neither": true,
		"i": true, "me": true, "my": true, "you": true, "your": true,
		"he": true, "him": true, "she": true, "her": true, "it": true,
		"we": true, "us": true, "they": true, "them": true,
		"this": true, "that": true, "these": true, "those": true,
		"what": true, "which": true, "who": true, "whom": true,
		"when": true, "where": true, "why": true, "how": true,
	}
	words := strings.Fields(text)
	var terms []string
	for _, w := range words {
		clean := strings.Trim(w, ".,!?;:\"'()[]{}")
		lower := strings.ToLower(clean)
		if !stopwords[lower] && len(clean) > 2 {
			terms = append(terms, clean)
		}
		if len(terms) >= 5 {
			break
		}
	}
	if len(terms) == 0 {
		return text
	}
	return strings.Join(terms, " ")
}

// ShouldRouteLocally checks if the task can be handled by a local model
// (fast/chat) vs requiring the bridge for tool calling (code/reasoning).
//
// With GLM-4-Flash as primary (free, supports tools), we route everything
// through the router which tries GLM first. Only return true for tasks
// that should skip tool calling entirely.
func ShouldRouteLocally(task TaskType) bool {
	return task == TaskFast || task == TaskChat
}

// LogPipelineTiming prints a breakdown of where time was spent.
// Call with defer after the pipeline completes.
func LogPipelineTiming(pc *PipelineContext) {
	elapsed := time.Since(pc.StartTime)
	log.Printf("[PIPELINE] run=%s task=%s tools=%v duration=%v",
		pc.RunID, pc.Task, pc.CalledTools, elapsed)
}

var _ = json.RawMessage{} // suppress unused import warning