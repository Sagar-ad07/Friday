package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────
// Terminal Tool
// ──────────────────────────────────────────────────────────────────────

type TerminalTool struct{}

func (t *TerminalTool) Name() string        { return "run_terminal" }
func (t *TerminalTool) Description() string { return "Run a shell command in sandbox (30s timeout)" }
func (t *TerminalTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"command": {Type: "string", Description: "Shell command to run"},
			"timeout": {Type: "number", Description: "Timeout in seconds", Default: 30},
			"dir":     {Type: "string", Description: "Working directory (default: sandbox)"},
		},
		Required: []string{"command"},
	}
}

func (t *TerminalTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
		Dir     string `json:"dir"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}
	if params.Timeout == 0 {
		params.Timeout = 30
	}
	if params.Timeout > 120 {
		params.Timeout = 120
	}

	// Security: block dangerous commands (case-insensitive, whitespace-normalized)
	blocked := []string{"format", "del", "rd", "rmdir", "reg delete", "shutdown"}
	normalized := strings.Join(strings.Fields(strings.ToLower(params.Command)), " ")
	for _, b := range blocked {
		if strings.Contains(normalized, b) {
			return nil, fmt.Errorf("command blocked: %s", b)
		}
	}

	workDir := SandboxDir()
	if params.Dir != "" {
		workDir = params.Dir
		// Security: ensure dir is within project root or sandbox
		rel, err := filepath.Rel(ProjectRoot, workDir)
		if err != nil || strings.HasPrefix(rel, "..") {
			return nil, fmt.Errorf("directory not allowed: %s", params.Dir)
		}
	}

	ctxTimeout, cancel := context.WithTimeout(ctx, time.Duration(params.Timeout)*time.Second)
	defer cancel()

	// Use PowerShell for better command handling (handles quoting, pipes, etc.)
	cmd := exec.CommandContext(ctxTimeout, "powershell", "-NoProfile", "-Command", params.Command)
	cmd.Dir = workDir

	output, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	// Limit output size to 100KB to prevent memory exhaustion
	outputStr := string(output)
	if len(outputStr) > 100*1024 {
		outputStr = outputStr[:100*1024] + "\n... [truncated at 100KB]"
	}

	return map[string]any{
		"stdout":    outputStr,
		"exit_code": exitCode,
		"command":   params.Command,
	}, nil
}

// ──────────────────────────────────────────────────────────────────────
// Code Tool (Python execution in sandbox)
// ──────────────────────────────────────────────────────────────────────

type CodeTool struct{}

func (t *CodeTool) Name() string        { return "run_code" }
func (t *CodeTool) Description() string { return "Run Python code in sandbox (30s timeout)" }
func (t *CodeTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"code":    {Type: "string", Description: "Python code to execute"},
			"timeout": {Type: "number", Description: "Timeout in seconds", Default: 30},
		},
		Required: []string{"code"},
	}
}

func (t *CodeTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Code    string `json:"code"`
		Timeout int    `json:"timeout"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}
	if params.Timeout == 0 {
		params.Timeout = 30
	}

	ctxTimeout, cancel := context.WithTimeout(ctx, time.Duration(params.Timeout)*time.Second)
	defer cancel()

	// Write code to temp file
	tmpDir := SandboxDir()
	tmpFile := filepath.Join(tmpDir, fmt.Sprintf("friday_code_%d.py", time.Now().UnixNano()))
	if err := os.WriteFile(tmpFile, []byte(params.Code), 0644); err != nil {
		return nil, err
	}
	defer os.Remove(tmpFile)

	cmd := exec.CommandContext(ctxTimeout, "python", tmpFile)
	cmd.Dir = tmpDir

	output, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	return map[string]any{
		"stdout":    string(output),
		"exit_code": exitCode,
	}, nil
}

// ──────────────────────────────────────────────────────────────────────
// File Tool
// ──────────────────────────────────────────────────────────────────────

type FileTool struct{}

func (t *FileTool) Name() string        { return "manage_files" }
func (t *FileTool) Description() string { return "Manage files (list, read, write, delete) in sandbox" }
func (t *FileTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"action":  {Type: "string", Enum: []string{"list", "read", "write", "delete", "mkdir", "create", "create_folder", "move", "rename", "copy"}, Description: "Action to perform"},
			"path":    {Type: "string", Description: "File path (relative to sandbox, or absolute with 'project:' prefix)"},
			"content": {Type: "string", Description: "Content for write action"},
		},
		Required: []string{"action", "path"},
	}
}

func (t *FileTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Action  string `json:"action"`
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	// Determine base directory: "project:" prefix for project root, otherwise sandbox
	var base string
	if strings.HasPrefix(params.Path, "project:") {
		base = ProjectRoot
		params.Path = strings.TrimPrefix(params.Path, "project:")
	} else {
		base = SandboxDir()
	}

	fullPath := filepath.Join(base, params.Path)

	// Security: ensure path stays within allowed base
	rel, err := filepath.Rel(base, fullPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil, fmt.Errorf("path traversal not allowed")
	}

	switch params.Action {
	case "list":
		entries, err := os.ReadDir(fullPath)
		if err != nil {
			return nil, err
		}
		files := make([]map[string]any, len(entries))
		for i, e := range entries {
			info, _ := e.Info()
			files[i] = map[string]any{
				"name":    e.Name(),
				"size":    info.Size(),
				"isDir":   e.IsDir(),
				"modTime": info.ModTime().Format(time.RFC3339),
			}
		}
		return map[string]any{"files": files}, nil

	case "read":
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, err
		}
		return map[string]any{"content": string(data), "path": params.Path}, nil

	case "mkdir", "create", "create_folder":
		if err := os.MkdirAll(fullPath, 0755); err != nil {
			return nil, err
		}
		return map[string]any{"success": true, "path": params.Path, "action": "created"}, nil

	case "write":
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(fullPath, []byte(params.Content), 0644); err != nil {
			return nil, err
		}
		return map[string]any{"success": true, "path": params.Path, "action": "written"}, nil

	case "delete":
		if err := os.Remove(fullPath); err != nil {
			return nil, err
		}
		return map[string]any{"success": true, "path": params.Path, "action": "deleted"}, nil

	case "move", "rename":
		dest := filepath.Join(filepath.Dir(fullPath), params.Content)
		if err := os.Rename(fullPath, dest); err != nil {
			return nil, err
		}
		return map[string]any{"success": true, "from": params.Path, "to": filepath.Base(dest)}, nil

	case "copy":
		dest := filepath.Join(filepath.Dir(fullPath), params.Content)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(dest, data, 0644); err != nil {
			return nil, err
		}
		return map[string]any{"success": true, "from": params.Path, "to": filepath.Base(dest)}, nil

	default:
		return nil, fmt.Errorf("unknown action: %s", params.Action)
	}
}

func SandboxDir() string {
	dir := filepath.Join(ProjectRoot, "data", "sandbox")
	os.MkdirAll(dir, 0755)
	return dir
}

// ──────────────────────────────────────────────────────────────────────
// Web Search Tool
// ──────────────────────────────────────────────────────────────────────

type WebSearchTool struct{}

func (t *WebSearchTool) Name() string        { return "web_search" }
func (t *WebSearchTool) Description() string { return "Search the web for information" }
func (t *WebSearchTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"query": {Type: "string", Description: "Search query"},
			"limit": {Type: "number", Description: "Max results", Default: 5},
		},
		Required: []string{"query"},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}
	if params.Limit == 0 || params.Limit > 20 {
		params.Limit = 10
	}

	// DuckDuckGo Lite search — free, no API key required
	form := url.Values{"q": {params.Query}}
	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://lite.duckduckgo.com/lite/", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("search request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	html := string(body)

	// Parse results from DuckDuckGo Lite HTML
	type Result struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Snippet string `json:"snippet"`
	}

	var results []Result

	// Parse the modern DDG result format
	urlRe := regexp.MustCompile(`class="result-link"[^>]*href="([^"]*)"`)
	urlMatches := urlRe.FindAllStringSubmatch(html, -1)

	if len(urlMatches) > 0 {
		titleRe := regexp.MustCompile(`class="result-link"[^>]*>(.*?)</a>`)
		titleMatches := titleRe.FindAllStringSubmatch(html, -1)
		snipRe2 := regexp.MustCompile(`class="result-snippet"[^>]*>(.*?)</td>`)
		snipMatches2 := snipRe2.FindAllStringSubmatch(html, -1)

		for i, m := range urlMatches {
			if len(results) >= params.Limit {
				break
			}
			r := Result{URL: cleanURL(m[1])}
			if i < len(titleMatches) {
				r.Title = stripTags(titleMatches[i][1])
			}
			if i < len(snipMatches2) {
				r.Snippet = stripTags(snipMatches2[i][1])
			}
			if r.Title != "" || r.URL != "" {
				results = append(results, r)
			}
		}
	}

	// Fallback: scrape any href
	if len(results) == 0 {
		allLinks := regexp.MustCompile(`<a[^>]*href="(https?://[^"]+)"[^>]*>(.*?)</a>`)
		linkMatches := allLinks.FindAllStringSubmatch(html, -1)
		seen := make(map[string]bool)
		for _, m := range linkMatches {
			if len(results) >= params.Limit {
				break
			}
			u := cleanURL(m[1])
			if seen[u] || strings.Contains(u, "duckduckgo.com") {
				continue
			}
			seen[u] = true
			results = append(results, Result{
				Title: stripTags(m[2]),
				URL:   u,
			})
		}
	}

	if results == nil {
		results = []Result{}
	}

	return map[string]any{
		"query":   params.Query,
		"results": results,
		"count":   len(results),
	}, nil
}

// ──────────────────────────────────────────────────────────────────────
// Web Fetch Tool (read web page content)
// ──────────────────────────────────────────────────────────────────────

type WebFetchTool struct{}

func (t *WebFetchTool) Name() string        { return "web_fetch" }
func (t *WebFetchTool) Description() string { return "Fetch and read the content of a web page" }
func (t *WebFetchTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"url":        {Type: "string", Description: "URL to fetch"},
			"max_chars":  {Type: "number", Description: "Max characters to return", Default: 5000},
		},
		Required: []string{"url"},
	}
}

func (t *WebFetchTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		URL      string `json:"url"`
		MaxChars int    `json:"max_chars"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}
	if params.MaxChars == 0 || params.MaxChars > 50000 {
		params.MaxChars = 5000
	}

	req, err := http.NewRequestWithContext(ctx, "GET", params.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "text/html,text/plain,*/*")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	text := string(body)

	// Strip HTML tags for readability
	text = stripTags(text)

	// Collapse whitespace
	space := regexp.MustCompile(`\s+`)
	text = space.ReplaceAllString(text, " ")
	text = strings.TrimSpace(text)

	if len(text) > params.MaxChars {
		text = text[:params.MaxChars] + "...\n[truncated]"
	}

	return map[string]any{
		"url":     params.URL,
		"content": text,
		"length":  len(text),
		"status":  resp.StatusCode,
	}, nil
}

// ──────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────

// KlineData is a simplified kline for market analysis
type KlineData struct {
	Open  float64
	High  float64
	Low   float64
	Close float64
}

func toFloat(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	case int:
		return float64(val)
	}
	return 0
}

func calcRSI(prices []float64, period int) float64 {
	if len(prices) < period+1 {
		return 50
	}
	gains, losses := 0.0, 0.0
	for i := 1; i <= period; i++ {
		diff := prices[i] - prices[i-1]
		if diff > 0 {
			gains += diff
		} else {
			losses -= diff
		}
	}
	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)
	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}

func avg(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func stripTags(s string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	s = re.ReplaceAllString(s, " ")
	re2 := regexp.MustCompile(`\s+`)
	s = re2.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func cleanURL(u string) string {
	if strings.HasPrefix(u, "//") {
		u = "https:" + u
	}
	u = strings.ReplaceAll(u, "&amp;", "&")
	u = strings.ReplaceAll(u, "&lt;", "<")
	u = strings.ReplaceAll(u, "&gt;", ">")
	u = strings.ReplaceAll(u, "&quot;", "\"")
	return u
}
