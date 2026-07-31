package friday

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type decisionRecord struct {
	Query  string   `json:"query"`
	Tools  []string `json:"tools"`
	Count  int      `json:"count"`
	Last   time.Time `json:"last"`
}

type decisionMemory struct {
	mu      sync.RWMutex
	records []decisionRecord
	path    string
	dirty   bool
}

var globalDecisionMemory *decisionMemory
var decisionOnce sync.Once

func getDecisionMemory() *decisionMemory {
	decisionOnce.Do(func() {
		path := filepath.Join(ProjectRoot, "data", "decisions.json")
		dm := &decisionMemory{path: path}
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, &dm.records)
		}
		globalDecisionMemory = dm
	})
	return globalDecisionMemory
}

func (dm *decisionMemory) save() {
	data, _ := json.MarshalIndent(dm.records, "", "  ")
	os.WriteFile(dm.path, data, 0644)
}

// Learn records which tools were used for a query
func (dm *decisionMemory) Learn(query string, tools []string) {
	if len(tools) == 0 { return }
	q := strings.ToLower(strings.TrimSpace(query))
	dm.mu.Lock()
	defer dm.mu.Unlock()

	for i, r := range dm.records {
		if strings.Contains(q, strings.ToLower(r.Query)) || strings.Contains(strings.ToLower(r.Query), q) {
			dm.records[i].Count++
			dm.records[i].Last = time.Now()
			// Add new tools not already recorded
			seen := map[string]bool{}
			for _, t := range dm.records[i].Tools { seen[t] = true }
			for _, t := range tools {
				if !seen[t] { dm.records[i].Tools = append(dm.records[i].Tools, t) }
			}
			dm.dirty = true
			return
		}
	}

	dm.records = append(dm.records, decisionRecord{
		Query: q, Tools: tools, Count: 1, Last: time.Now(),
	})
	if len(dm.records) > 200 {
		dm.records = dm.records[len(dm.records)-200:]
	}
	dm.dirty = true
}

// Suggest returns tools known to work for a query, plus confidence (0.0-1.0)
func (dm *decisionMemory) Suggest(query string) ([]string, float64) {
	q := strings.ToLower(strings.TrimSpace(query))
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	bestScore := 0.0
	var bestTools []string
	for _, r := range dm.records {
		score := 0.0
		rWords := strings.Fields(r.Query)
		qWords := strings.Fields(q)
		if len(rWords) == 0 || len(qWords) == 0 { continue }

		matches := 0
		for _, rw := range rWords {
			for _, qw := range qWords {
				if rw == qw || strings.Contains(rw, qw) || strings.Contains(qw, rw) {
					matches++
					break
				}
			}
		}
		score = float64(matches) / float64(len(rWords))
		if score > bestScore {
			bestScore = score
			bestTools = r.Tools
		}
	}
	return bestTools, bestScore
}

func (dm *decisionMemory) SaveIfDirty() {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	if dm.dirty {
		data, _ := json.MarshalIndent(dm.records, "", "  ")
		os.WriteFile(dm.path, data, 0644)
		dm.dirty = false
	}
}
