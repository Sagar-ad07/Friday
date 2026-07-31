package friday

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync/atomic"

	"github.com/friday-prototype/friday-go/pkg/db"
)

// MaxRecentMessages is how many recent turns stay verbatim in the context
// window. Everything older gets folded into a single summary line.
const MaxRecentMessages = 6

// ManagedContext builds the message list for an LLM call:
//   - the system prompt always leads
//   - short histories pass through untouched
//   - long histories keep only the last MaxRecentMessages turns verbatim
//     and squash the rest into one synthetic summary message, so the model
//     never drowns in old back-and-forth
func ManagedContext(history []Message, systemPrompt string) []Message {
	head := []Message{{Role: "system", Content: systemPrompt}}
	if len(history) == 0 {
		return head
	}

	if len(history) <= MaxRecentMessages {
		return append(head, history...)
	}

	older := history[:len(history)-MaxRecentMessages]
	recent := history[len(history)-MaxRecentMessages:]

	var sb strings.Builder
	fmt.Fprintf(&sb, "[Earlier conversation — %d messages summarized]", len(older))
	withSummary := []Message{{Role: "system", Content: sb.String()}}
	return append(append(head, withSummary...), recent...)
}

// memoryStats tracks how many semantic recalls hit the cache vs the DB.
var memoryStats struct {
	recalls atomic.Int64
}

// RecallRelevantMemory queries the semantic memory table for facts matching
// the key terms and returns them formatted as a bullet list the pipeline can
// inject straight into the system context. Returns "" when there is nothing
// worth injecting.
func RecallRelevantMemory(ctx context.Context, terms string, limit int) string {
	words := strings.Fields(terms)
	if len(words) == 0 || limit <= 0 {
		return ""
	}

	matches := make([]string, 0, limit)
	seen := make(map[string]bool)

	// Build an FTS5 query from the key terms. Joining with AND keeps the
	// results on-topic; a single OR term would pull in noise.
	ftsQuery := strings.Join(words, " AND ")

	d := db.Get()
	if d != nil {
		rows, err := d.QueryContext(ctx,
			"SELECT fact, type, verified FROM memory_facts WHERE fact MATCH ? ORDER BY created_at DESC LIMIT ?",
			ftsQuery, limit)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var fact, memType string
				var verified int
				if rows.Scan(&fact, &memType, &verified) == nil && !seen[fact] {
					seen[fact] = true
					label := ""
					if memType == string(MemEpisodic) {
						label = " (from experience)"
					} else if verified == 1 {
						label = " (verified)"
					}
					matches = append(matches, fact+label)
				}
			}
		}
	}

	// Fall back to the legacy in-memory store when the DB is unavailable.
	if len(matches) == 0 {
		memMu.RLock()
		for _, f := range memStore {
			if len(matches) >= limit {
				break
			}
			haystack := strings.ToLower(f.Fact)
			hit := true
			for _, t := range words {
				if !strings.Contains(haystack, strings.ToLower(t)) {
					hit = false
					break
				}
			}
			if hit && !seen[f.Fact] {
				seen[f.Fact] = true
				matches = append(matches, f.Fact)
			}
		}
		memMu.RUnlock()
	}

	if len(matches) == 0 {
		return ""
	}

	memoryStats.recalls.Add(1)
	log.Printf("[PIPELINE] semantic recall: %d facts for terms %v", len(matches), words)
	return "\n- " + strings.Join(matches, "\n- ")
}
