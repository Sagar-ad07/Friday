package friday

import (
	"fmt"
	"log"
	"regexp"
	"sync"
	"time"
)

// SmartGateConfig represents smart gate configuration
type SmartGateConfig struct {
	Enabled         bool     `json:"enabled"`
	MinConfidence   float64  `json:"minConfidence"`
	MaxFailures     int      `json:"maxFailures"`
	WindowSize      int      `json:"windowSize"`
	CheckPatterns   []string `json:"checkPatterns"`
	BlockPatterns   []string `json:"blockPatterns"`
	WarningPatterns []string `json:"warningPatterns"`
}

// SmartGate represents the pattern-based smart gate system
type SmartGate struct {
	config   SmartGateConfig
	failures []GateFailure
	mu       sync.RWMutex
}

// GateFailure represents a gate failure
type GateFailure struct {
	ID         string    `json:"id"`
	Time       time.Time `json:"time"`
	Operation  string    `json:"operation"`
	Reason     string    `json:"reason"`
	Confidence float64   `json:"confidence"`
	AutoBlocked bool     `json:"autoBlocked"`
}

// GateDecision represents the result of a gate check
type GateDecision struct {
	Approved   bool   `json:"approved"`
	Confidence float64 `json:"confidence"`
	Reason     string `json:"reason"`
	Suggestion string `json:"suggestion"`
	AutoBlocked bool  `json:"autoBlocked"`
}

// NewSmartGate creates a new smart gate instance
func NewSmartGate() *SmartGate {
	return &SmartGate{
		config: SmartGateConfig{
			Enabled:       true,
			MinConfidence: 0.85,
			MaxFailures:   5,
			WindowSize:    100,
			CheckPatterns: []string{
				`func\s+\w+\(`,
				`import\s+\(`,
				`http\.HandleFunc`,
				`router\.(GET|POST|PUT|DELETE|PATCH)`,
				`go build`,
			},
			BlockPatterns: []string{
				`rm\s+-rf`,
				`rm\s+-rf\s+/`,
				`format\s+C:\:`,
				`del\s+.*friday`,
				`rm\s+.*friday.exe`,
			},
			WarningPatterns: []string{
				`fmt\.Sprintf.*password`,
				`log\.Print.*secret`,
				`os\.Setenv.*KEY`,
			},
		},
		failures: []GateFailure{},
	}
}

// AnalyzeChange analyzes a code change using patterns
func (sg *SmartGate) AnalyzeChange(operation string, files []string, diff string) (GateDecision, error) {
	sg.mu.Lock()
	defer sg.mu.Unlock()

	decision := sg.analyzePatterns(operation, diff)

	if sg.isRepeatFailure(operation) {
		decision.Confidence *= 0.8
		decision.Reason += " [repeat failure detected]"
	}

	if !decision.Approved {
		failure := GateFailure{
			ID:         fmt.Sprintf("%d", time.Now().UnixNano()),
			Time:       time.Now(),
			Operation:  operation,
			Reason:     decision.Reason,
			Confidence: decision.Confidence,
			AutoBlocked: decision.AutoBlocked,
		}
		sg.failures = append(sg.failures, failure)
		if len(sg.failures) > sg.config.WindowSize {
			sg.failures = sg.failures[sg.config.WindowSize*-1:]
		}
	}

	return decision, nil
}

// analyzePatterns performs pattern-based analysis
func (sg *SmartGate) analyzePatterns(operation string, diff string) GateDecision {
	for _, pattern := range sg.config.BlockPatterns {
		re := regexp.MustCompile(pattern)
		if re.MatchString(diff) {
			return GateDecision{
				Approved:    false,
				Confidence:  1.0,
				Reason:      fmt.Sprintf("blocked by pattern: %s", pattern),
				AutoBlocked: true,
			}
		}
	}

	for _, pattern := range sg.config.WarningPatterns {
		re := regexp.MustCompile(pattern)
		if re.MatchString(diff) {
			return GateDecision{
				Approved:   true,
				Confidence: 0.6,
				Reason:     fmt.Sprintf("warning: matches pattern %s", pattern),
				Suggestion: "Review manually before applying",
			}
		}
	}

	for _, pattern := range sg.config.CheckPatterns {
		re := regexp.MustCompile(pattern)
		if re.MatchString(diff) {
			return GateDecision{
				Approved:   true,
				Confidence: 0.85,
				Reason:     fmt.Sprintf("matches expected pattern: %s", pattern),
			}
		}
	}

	return GateDecision{
		Approved:   true,
		Confidence: 0.5,
		Reason:     "no patterns matched, moderate confidence",
	}
}

// isRepeatFailure checks if this operation has failed too many times
func (sg *SmartGate) isRepeatFailure(operation string) bool {
	recent := 0
	window := time.Now().Add(-30 * time.Minute)
	for _, f := range sg.failures {
		if f.Operation == operation && f.Time.After(window) {
			recent++
		}
	}
	return recent >= sg.config.MaxFailures
}

// GetFailureHistory returns the gate failure history
func (sg *SmartGate) GetFailureHistory() []GateFailure {
	sg.mu.RLock()
	defer sg.mu.RUnlock()

	result := make([]GateFailure, len(sg.failures))
	copy(result, sg.failures)
	return result
}

// ClearFailures clears the failure history
func (sg *SmartGate) ClearFailures() {
	sg.mu.Lock()
	defer sg.mu.Unlock()
	sg.failures = []GateFailure{}
}

// SetConfig updates the smart gate configuration
func (sg *SmartGate) SetConfig(config SmartGateConfig) {
	sg.mu.Lock()
	defer sg.mu.Unlock()
	sg.config = config
}

// GlobalSmartGate is the global smart gate instance
var GlobalSmartGate *SmartGate

// InitSmartGate initializes the global smart gate
func InitSmartGate() {
	GlobalSmartGate = NewSmartGate()
	log.Println("✅ Smart Gate initialized")
}

// CheckSmartGate performs a smart gate check on an operation
func CheckSmartGate(operation string, files []string, diff string) (bool, string) {
	if GlobalSmartGate == nil {
		return true, "no smart gate configured"
	}

	decision, err := GlobalSmartGate.AnalyzeChange(operation, files, diff)
	if err != nil {
		return true, fmt.Sprintf("gate check error: %v (defaulting to approved)", err)
	}

	if !decision.Approved {
		return false, fmt.Sprintf("SMART GATE BLOCKED: %s (confidence: %.2f)", decision.Reason, decision.Confidence)
	}

	if decision.Suggestion != "" {
		log.Printf("SUGGESTION for %s: %s", operation, decision.Suggestion)
	}

	return true, fmt.Sprintf("approved (confidence: %.2f)", decision.Confidence)
}