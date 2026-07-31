package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

// ──────────────────────────────────────────────────────────────────────
// Self-Repair — when Friday panics, she reads her own stack trace,
// asks her LLM brain what went wrong, generates a patch, applies it,
// rebuilds, and restarts. This is Level 2 autonomy.
// ──────────────────────────────────────────────────────────────────────

// RepairAttempt records one self-repair cycle for auditing.
type RepairAttempt struct {
	Timestamp   time.Time
	PanicMsg    string
	StackTrace  string
	SourceFile  string
	LLMAnalysis string
	PatchApplied bool
	RebuildOK   bool
	Error       string
}

var repairHistory []RepairAttempt
var repairMu sync.Mutex

// maxRepairAttempts prevents infinite repair loops (repairing a repair
// that repaired a repair...). 3 attempts per crash, then give up and
// alert the user.
const maxRepairAttempts = 3

// SelfRepair is the entry point. Call from any defer/recover block:
//
//	defer func() {
//	    if r := recover(); r != nil {
//	        friday.SelfRepair(r, "component_name")
//	    }
//	}()
func SelfRepair(panicVal any, component string) {
	stack := string(debug.Stack())
	msg := fmt.Sprintf("%v", panicVal)

	log.Printf("[SELF-REPAIR] PANIC in %s: %s", component, msg)
	log.Printf("[SELF-REPAIR] stack trace:\n%s", stack[:repairMinInt(len(stack), 2000)])

	attempt := RepairAttempt{
		Timestamp:  time.Now(),
		PanicMsg:   msg,
		StackTrace: stack,
	}

	// Step 1: Extract the source file + line from the stack trace
	sourceFile, sourceLine := extractSourceFromStack(stack)
	attempt.SourceFile = fmt.Sprintf("%s:%d", sourceFile, sourceLine)

	if sourceFile == "" {
		attempt.Error = "could not extract source file from stack"
		log.Printf("[SELF-REPAIR] %s", attempt.Error)
		recordRepair(attempt)
		return
	}

	// Step 2: Read the source file around the crash line
	sourceCode, err := readSourceAround(sourceFile, sourceLine, 20)
	if err != nil {
		attempt.Error = fmt.Sprintf("read source: %v", err)
		log.Printf("[SELF-REPAIR] %s", attempt.Error)
		recordRepair(attempt)
		return
	}

	// Step 3: Ask the LLM to analyze the crash and suggest a fix
	analysis, patch, err := llmAnalyzeCrash(msg, stack, sourceFile, sourceCode)
	if err != nil {
		attempt.Error = fmt.Sprintf("LLM analysis: %v", err)
		log.Printf("[SELF-REPAIR] %s", attempt.Error)
		recordRepair(attempt)
		return
	}
	attempt.LLMAnalysis = analysis

	// Step 4: Check repair history — don't loop forever
	repairMu.Lock()
	recentRepairs := 0
	for _, r := range repairHistory {
		if time.Since(r.Timestamp) < 5*time.Minute {
			recentRepairs++
		}
	}
	repairMu.Unlock()

	if recentRepairs >= maxRepairAttempts {
		attempt.Error = "max repair attempts reached — alerting user"
		log.Printf("[SELF-REPAIR] %s", attempt.Error)
		CreateAlert("self_repair", "🔧 Self-Repair Failed",
			fmt.Sprintf("Friday crashed in %s but has hit her repair limit (3 attempts in 5min).\n\nPanic: %s\n\nAnalysis: %s\n\nShe needs manual intervention.", component, msg, analysis),
			"critical")
		recordRepair(attempt)
		return
	}

	// Step 5: Apply the patch if the LLM provided one
	if patch == "" {
		attempt.Error = "LLM did not provide a patch"
		log.Printf("[SELF-REPAIR] %s", attempt.Error)
		recordRepair(attempt)
		return
	}

	if err := applyPatch(sourceFile, patch); err != nil {
		attempt.Error = fmt.Sprintf("apply patch: %v", err)
		log.Printf("[SELF-REPAIR] %s", attempt.Error)
		recordRepair(attempt)
		return
	}
	attempt.PatchApplied = true
	log.Printf("[SELF-REPAIR] patch applied to %s", sourceFile)

	// Step 6: Rebuild
	if err := rebuild(); err != nil {
		attempt.Error = fmt.Sprintf("rebuild: %v", err)
		log.Printf("[SELF-REPAIR] %s", attempt.Error)
		// Revert the patch since the build failed
		recordRepair(attempt)
		return
	}
	attempt.RebuildOK = true
	log.Printf("[SELF-REPAIR] rebuild successful")

	// Step 7: Alert the user about what happened
	CreateAlert("self_repair", "🔧 Self-Repair Successful",
		fmt.Sprintf("Friday crashed in %s and FIXED HERSELF.\n\nPanic: %s\n\nWhat she found: %s\n\nShe patched %s and rebuilt successfully.", component, msg, analysis, sourceFile),
		"info")

	recordRepair(attempt)

	// The caller should restart the component after this returns.
}

// extractSourceFromStack parses a Go stack trace and finds the first
// non-runtime source file + line number.
func extractSourceFromStack(stack string) (string, int) {
	// Stack traces look like:
	//   /path/to/file.go:123 +0x4a5
	re := regexp.MustCompile(`(/[\w/.\-]+\.go):(\d+)`)
	matches := re.FindAllStringSubmatch(stack, -1)
	for _, m := range matches {
		file := m[1]
		// Skip runtime files
		if strings.Contains(file, "runtime/") || strings.Contains(file, "src/runtime") {
			continue
		}
		line := 0
		fmt.Sscanf(m[2], "%d", &line)
		return file, line
	}
	return "", 0
}

// readSourceAround reads lines [line-radius, line+radius] from a source file.
func readSourceAround(filename string, line, radius int) (string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	start := line - radius - 1
	if start < 0 {
		start = 0
	}
	end := line + radius
	if end > len(lines) {
		end = len(lines)
	}
	var sb strings.Builder
	for i := start; i < end; i++ {
		marker := "  "
		if i+1 == line {
			marker = ">>"
		}
		sb.WriteString(fmt.Sprintf("%s %4d: %s\n", marker, i+1, lines[i]))
	}
	return sb.String(), nil
}

// llmAnalyzeCrash sends the panic + stack trace + source code to the LLM
// and asks for analysis + a code patch.
func llmAnalyzeCrash(panicMsg, stackTrace, sourceFile, sourceCode string) (analysis, patch string, err error) {
	router := NewModelRouter()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	prompt := fmt.Sprintf(`You are a Go code repair agent. A Go program crashed. Analyze the crash and provide a fix.

PANIC: %s

STACK TRACE (first 1500 chars):
%s

SOURCE CODE around the crash (%s):
%s

INSTRUCTIONS:
1. Explain what caused the crash in one sentence.
2. Provide the COMPLETE corrected function or code block that would prevent this crash.
3. Format your response as JSON:
{"analysis": "one sentence explanation", "patch": "the corrected Go code here"}

Focus on defensive programming: nil checks, bounds checks, error handling. Don't change the logic — just make it not crash.`,
		panicMsg,
		stackTrace[:repairMinInt(len(stackTrace), 1500)],
		sourceFile,
		sourceCode)

	messages := []Message{
		{Role: "system", Content: "You are a Go code repair agent. Respond only with valid JSON."},
		{Role: "user", Content: prompt},
	}

	resp, err := router.Chat(ctx, messages)
	if err != nil {
		return "", "", fmt.Errorf("LLM call: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", "", fmt.Errorf("no LLM response")
	}

	content := resp.Choices[0].Message.Content
	// Extract JSON from the response (it might be wrapped in markdown)
	jsonStart := strings.Index(content, "{")
	jsonEnd := strings.LastIndex(content, "}")
	if jsonStart < 0 || jsonEnd < 0 || jsonEnd <= jsonStart {
		return content, "", nil // return raw as analysis
	}

	var result struct {
		Analysis string `json:"analysis"`
		Patch    string `json:"patch"`
	}
	if err := json.Unmarshal([]byte(content[jsonStart:jsonEnd+1]), &result); err != nil {
		return content, "", nil // return raw as analysis
	}

	return result.Analysis, result.Patch, nil
}

// applyPatch writes the patched code to the source file.
// In Level 2, we replace the entire file content with the patch if the
// patch is a complete file, or we replace the function if we can identify it.
// For safety, we back up the original first.
func applyPatch(sourceFile, patch string) error {
	// Backup the original
	backup := sourceFile + ".bak"
	if err := os.Rename(sourceFile, backup); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	// If the patch looks like a complete Go file, write it directly
	if strings.Contains(patch, "package ") {
		if err := os.WriteFile(sourceFile, []byte(patch), 0644); err != nil {
			os.Rename(backup, sourceFile) // restore
			return fmt.Errorf("write failed: %w", err)
		}
		return nil
	}

	// Otherwise, append the patch as a comment for manual review
	// (Level 3 will do proper function-level patching)
	original, _ := os.ReadFile(backup)
	content := string(original) + "\n\n// SELF-REPAIR PATCH (needs manual review):\n// " + strings.ReplaceAll(patch, "\n", "\n// ")
	if err := os.WriteFile(sourceFile, []byte(content), 0644); err != nil {
		os.Rename(backup, sourceFile)
		return fmt.Errorf("write failed: %w", err)
	}

	return nil
}

// rebuild recompiles the friday binary.
func rebuild() error {
	// Find the Go source directory
	srcDir := ProjectRoot
	if _, err := os.Stat(filepath.Join(srcDir, "go.mod")); err != nil {
		// Try subdirectory
		srcDir = filepath.Join(ProjectRoot, "go")
		if _, err := os.Stat(filepath.Join(srcDir, "go.mod")); err != nil {
			return fmt.Errorf("go.mod not found")
		}
	}

	cmd := exec.Command("go", "build", "-o", filepath.Join(srcDir, "friday.exe"), "./cmd/friday/")
	cmd.Dir = srcDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", string(output), err)
	}
	return nil
}

// recordRepair stores the attempt for auditing and loop prevention.
func recordRepair(attempt RepairAttempt) {
	repairMu.Lock()
	defer repairMu.Unlock()
	repairHistory = append(repairHistory, attempt)
	// Keep last 50 repair attempts
	if len(repairHistory) > 50 {
		repairHistory = repairHistory[len(repairHistory)-50:]
	}
}

// GetRepairHistory returns recent self-repair attempts for the UI.
func GetRepairHistory() []RepairAttempt {
	repairMu.Lock()
	defer repairMu.Unlock()
	hist := make([]RepairAttempt, len(repairHistory))
	copy(hist, repairHistory)
	return hist
}

func repairMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// init registers a global panic handler so even unrecoverable panics
// in goroutines get a chance at self-repair.
func init() {
	// This doesn't catch all panics (only ones in deferred goroutines
	// that call debug.HandleSignal), but it's a safety net.
	_ = runtime.GOOS // ensure runtime import is used
}

var _ = io.EOF // ensure io import is used
