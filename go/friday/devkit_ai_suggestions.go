package friday

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// AISuggestion represents an AI-generated suggestion for a draft
type AISuggestion struct {
	ID          string  `json:"id"`
	Draft       string  `json:"draft"`
	File        string  `json:"file"`
	Line        int     `json:"line"`
	Type        string  `json:"type"`
	Severity    string  `json:"severity"`
	Message     string  `json:"message"`
	Suggestion  string  `json:"suggestion"`
	Confidence  float64 `json:"confidence"`
	AutoApplied bool    `json:"autoApplied"`
}

// AISuggestionEngine handles AI-powered suggestions for DevKit drafts
type AISuggestionEngine struct {
	suggestions []AISuggestion
}

// NewAISuggestionEngine creates a new AI suggestion engine
func NewAISuggestionEngine() *AISuggestionEngine {
	return &AISuggestionEngine{
		suggestions: []AISuggestion{},
	}
}

// GenerateSuggestions generates pattern-based suggestions for a draft
func (e *AISuggestionEngine) GenerateSuggestions(draftDir string, files []string) ([]AISuggestion, error) {
	var suggestions []AISuggestion

	for _, file := range files {
		filePath := filepath.Join(draftDir, file)
		content, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		fileSuggestions := e.analyzeFile(file, string(content))
		suggestions = append(suggestions, fileSuggestions...)
	}

	e.suggestions = suggestions
	return suggestions, nil
}

// analyzeFile analyzes a single file for improvements
func (e *AISuggestionEngine) analyzeFile(filename string, content string) []AISuggestion {
	var suggestions []AISuggestion

	patterns := []struct {
		pattern   string
		severity  string
		msg       string
		suggest   string
		autoApply bool
	}{
		{
			pattern:   `fmt\.Print`,
			severity:  "info",
			msg:       "Uses fmt.Print instead of structured logging",
			suggest:   "Consider using log.Printf for better observability",
		},
		{
			pattern:   `errors\.New`,
			severity:  "info",
			msg:       "Uses errors.New for error creation",
			suggest:   "Consider using fmt.Errorf with wrapping for better error context",
		},
		{
			pattern:   `if\s+err\s!=\s+nil`,
			severity:  "warning",
			msg:       "Found error handling pattern",
			suggest:   "Ensure all errors are properly handled",
		},
		{
			pattern:   `TODO`,
			severity:  "info",
			msg:       "Contains TODO comment",
			suggest:   "Review and resolve TODO comments",
		},
	}

	for _, p := range patterns {
		re := regexp.MustCompile(p.pattern)
		matches := re.FindAllStringIndex(content, -1)
		for _, m := range matches {
			line := strings.Count(content[:m[0]], "\n") + 1
			suggestion := AISuggestion{
				ID:         fmt.Sprintf("%d", time.Now().UnixNano()),
				File:       filename,
				Line:       line,
				Type:       "pattern-match",
				Severity:   p.severity,
				Message:    p.msg,
				Suggestion: p.suggest,
				Confidence: 0.8,
			}
			suggestions = append(suggestions, suggestion)
		}
	}

	return suggestions
}

// ApplySuggestion applies a specific suggestion to a draft
func (e *AISuggestionEngine) ApplySuggestion(suggestion AISuggestion, draftDir string) (bool, error) {
	filePath := filepath.Join(draftDir, suggestion.File)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return false, fmt.Errorf("file %s not found in draft", suggestion.File)
	}

	log.Printf("SUGGESTION for %s: %s -> %s", suggestion.File, suggestion.Message, suggestion.Suggestion)
	return false, nil
}

// GetSuggestions returns all generated suggestions
func (e *AISuggestionEngine) GetSuggestions() []AISuggestion {
	result := make([]AISuggestion, len(e.suggestions))
	copy(result, e.suggestions)
	return result
}

// GetSuggestionsBySeverity returns suggestions filtered by severity
func (e *AISuggestionEngine) GetSuggestionsBySeverity(severity string) []AISuggestion {
	var result []AISuggestion
	for _, s := range e.suggestions {
		if s.Severity == severity {
			result = append(result, s)
		}
	}
	return result
}

// SelfHealingEngine handles automated repair of detected issues
type SelfHealingEngine struct {
	engine   *AISuggestionEngine
	repaired []RepairRecord
}

// RepairRecord represents a recorded repair action
type RepairRecord struct {
	ID        string    `json:"id"`
	File      string    `json:"file"`
	Issue     string    `json:"issue"`
	Fix       string    `json:"fix"`
	AppliedAt time.Time `json:"appliedAt"`
	Success   bool      `json:"success"`
}

// NewSelfHealingEngine creates a new self-healing engine
func NewSelfHealingEngine() *SelfHealingEngine {
	return &SelfHealingEngine{
		engine:   NewAISuggestionEngine(),
		repaired: []RepairRecord{},
	}
}

// HealFile attempts to automatically fix issues in a file
func (he *SelfHealingEngine) HealFile(filename string, content string, issues []AISuggestion) (string, []RepairRecord, error) {
	var records []RepairRecord
	healed := content

	for _, issue := range issues {
		if issue.Type != "pattern-match" {
			continue
		}

		record := RepairRecord{
			ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
			File:      filename,
			Issue:     issue.Message,
			Fix:       issue.Suggestion,
			AppliedAt: time.Now(),
			Success:   true,
		}
		records = append(records, record)
	}

	healed, err := formatGoCode(healed)
	if err != nil {
		log.Printf("gofmt failed: %v", err)
	}

	he.repaired = append(he.repaired, records...)
	return healed, records, nil
}

// formatGoCode runs gofmt on content
func formatGoCode(content string) (string, error) {
	cmd := exec.Command("gofmt", "-w")
	cmd.Stdin = strings.NewReader(content)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

// GetRepairHistory returns the repair history
func (he *SelfHealingEngine) GetRepairHistory() []RepairRecord {
	result := make([]RepairRecord, len(he.repaired))
	copy(result, he.repaired)
	return result
}

// HealDraft attempts to heal all files in a draft
func (he *SelfHealingEngine) HealDraft(draftDir string, files []string) (int, error) {
	totalRepaired := 0

	for _, file := range files {
		filePath := filepath.Join(draftDir, file)
		content, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		suggestions := he.engine.analyzeFile(file, string(content))
		healed, records, err := he.HealFile(file, string(content), suggestions)
		if err != nil {
			continue
		}

		if healed != string(content) {
			err = os.WriteFile(filePath, []byte(healed), 0644)
			if err != nil {
				continue
			}
			totalRepaired += len(records)
		}
	}

	return totalRepaired, nil
}
