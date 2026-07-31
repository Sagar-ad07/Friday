package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────
// Calc Tool
// ──────────────────────────────────────────────────────────────────────

type CalcTool struct{}

func (t *CalcTool) Name() string        { return "calc" }
func (t *CalcTool) Description() string { return "Evaluate mathematical expression safely" }
func (t *CalcTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"expression": {Type: "string", Description: "Math expression to evaluate"},
		},
		Required: []string{"expression"},
	}
}

func (t *CalcTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Expression string `json:"expression"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	result, err := evalMath(params.Expression)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	return result, nil
}

// ──────────────────────────────────────────────────────────────────────
// Time Tool
// ──────────────────────────────────────────────────────────────────────

type TimeTool struct{}

func (t *TimeTool) Name() string        { return "get_time" }
func (t *TimeTool) Description() string { return "Get current time in various formats" }
func (t *TimeTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"timezone": {Type: "string", Description: "Timezone (e.g., UTC, Local, America/New_York)", Default: "UTC"},
		},
		Required: []string{},
	}
}

func (t *TimeTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Timezone string `json:"timezone"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %v", err)
	}

	now := time.Now()
	loc := time.UTC
	if params.Timezone != "" && params.Timezone != "UTC" {
		if l, err := time.LoadLocation(params.Timezone); err == nil {
			loc = l
		}
	}
	now = now.In(loc)

	return map[string]any{
		"unix":      now.Unix(),
		"unix_ms":   now.UnixMilli(),
		"rfc3339":   now.Format(time.RFC3339),
		"iso8601":   now.Format(time.RFC3339),
		"readable":  now.Format("Monday, January 2, 2006 3:04:05 PM MST"),
		"timezone":  loc.String(),
	}, nil
}

// ──────────────────────────────────────────────────────────────────────
// System Info Tool
// ──────────────────────────────────────────────────────────────────────

type SystemInfoTool struct{}

func (t *SystemInfoTool) Name() string        { return "system_info" }
func (t *SystemInfoTool) Description() string { return "Get system information" }
func (t *SystemInfoTool) Schema() ToolSchema {
	return ToolSchema{
		Type:       "object",
		Properties: map[string]PropertyDef{},
		Required:   []string{},
	}
}

func (t *SystemInfoTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	result := map[string]any{
		"go_version":   runtime.Version(),
		"go_os":        runtime.GOOS,
		"go_arch":      runtime.GOARCH,
		"cpus":         runtime.NumCPU(),
		"goroutines":   runtime.NumGoroutine(),
		"mem_alloc_mb": m.Alloc / 1024 / 1024,
		"mem_sys_mb":   m.Sys / 1024 / 1024,
		"mem_gc_count": m.NumGC,
	}

	// Add real system memory from Windows API
	totalMemMB, availMemMB := getWindowsMemory()
	if totalMemMB > 0 {
		result["ram_total_mb"] = totalMemMB
		result["ram_avail_mb"] = availMemMB
		result["ram_used_mb"] = totalMemMB - availMemMB
		result["ram_used_pct"] = (totalMemMB - availMemMB) / totalMemMB * 100
	}

	// Add real disk usage from Windows API
	totalDiskGB, freeDiskGB := getWindowsDiskFree("C:\\")
	if totalDiskGB > 0 {
		result["disk_c_total_gb"] = totalDiskGB
		result["disk_c_free_gb"] = freeDiskGB
	}
	totalDiskD, freeDiskD := getWindowsDiskFree("D:\\")
	if totalDiskD > 0 {
		result["disk_d_total_gb"] = totalDiskD
		result["disk_d_free_gb"] = freeDiskD
	}

	return result, nil
}

// ──────────────────────────────────────────────────────────────────────
// Web Search Tool (Brave Search API)
// ──────────────────────────────────────────────────────────────────────

type BraveSearchTool struct{}

func (t *BraveSearchTool) Name() string { return "brave_search" }
func (t *BraveSearchTool) Description() string { return "Search using Brave Search API (set BRAVE_SEARCH_API_KEY env var for free tier)" }
func (t *BraveSearchTool) Schema() ToolSchema {
	return ToolSchema{Type: "object", Properties: map[string]PropertyDef{
		"query": {Type: "string", Description: "Search query"},
		"limit": {Type: "number", Description: "Max results", Default: 5},
	}, Required: []string{"query"}}
}
func (t *BraveSearchTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct{ Query string; Limit int }
	if err := json.Unmarshal(args, &p); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }
	if p.Limit == 0 || p.Limit > 20 { p.Limit = 5 }

	apiKey := os.Getenv("BRAVE_SEARCH_API_KEY")
	if apiKey == "" {
		return map[string]any{"error": "BRAVE_SEARCH_API_KEY not set. Get free key at https://brave.com/search/api/", "query": p.Query}, nil
	}

	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d", url.QueryEscape(p.Query), p.Limit), nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("X-Subscription-Token", apiKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return map[string]any{"error": err.Error(), "query": p.Query}, nil
	}
	defer resp.Body.Close()

	var data map[string]any
	json.NewDecoder(resp.Body).Decode(&data)
	return map[string]any{"source": "Brave Search", "query": p.Query, "results": data}, nil
}

// ──────────────────────────────────────────────────────────────────────
// Mojeek Search (free tier, needs MOJEEK_API_KEY env var)
// ──────────────────────────────────────────────────────────────────────

type MojeekSearchTool struct{}
func (t *MojeekSearchTool) Name() string { return "mojeek_search" }
func (t *MojeekSearchTool) Description() string { return "Search using Mojeek search engine (set MOJEEK_API_KEY env var for free tier)" }
func (t *MojeekSearchTool) Schema() ToolSchema {
	return ToolSchema{Type:"object", Properties:map[string]PropertyDef{
		"query": {Type:"string", Description:"Search query"},
		"limit": {Type:"number", Description:"Max results", Default:5},
	}, Required:[]string{"query"}}
}
func (t *MojeekSearchTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct{ Query string; Limit int }
	if err := json.Unmarshal(args, &p); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }
	if p.Limit == 0 || p.Limit > 20 { p.Limit = 5 }

	apiKey := os.Getenv("MOJEEK_API_KEY")
	if apiKey == "" {
		return map[string]any{"error": "MOJEEK_API_KEY not set. Get free key at https://www.mojeek.com/services/search/api/", "query": p.Query}, nil
	}

	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("https://api.mojeek.com/search?q=%s&fmt=json&size=%d", url.QueryEscape(p.Query), p.Limit), nil)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return map[string]any{"error": err.Error(), "query": p.Query}, nil
	}
	defer resp.Body.Close()

	var data map[string]any
	json.NewDecoder(resp.Body).Decode(&data)
	return map[string]any{"source": "Mojeek", "query": p.Query, "results": data}, nil
}

// ──────────────────────────────────────────────────────────────────────
// Wikipedia Search
// ──────────────────────────────────────────────────────────────────────

type WikipediaTool struct{}
func (t *WikipediaTool) Name() string { return "wikipedia" }
func (t *WikipediaTool) Description() string { return "Search Wikipedia for a topic summary" }
func (t *WikipediaTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"query": {Type: "string", Description: "Topic to search"},
		},
		Required: []string{"query"},
	}
}
func (t *WikipediaTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct{ Query string `json:"query"` }
	if err := json.Unmarshal(args, &p); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }
	resp, err := http.Get("https://en.wikipedia.org/api/rest_v1/page/summary/" + url.PathEscape(p.Query))
	if err != nil { return nil, err }
	defer resp.Body.Close()
	if resp.StatusCode == 200 {
		var data map[string]any
		json.NewDecoder(resp.Body).Decode(&data)
		return data, nil
	}
	resp, err = http.Get("https://en.wikipedia.org/w/api.php?action=query&list=search&srsearch=" + url.QueryEscape(p.Query) + "&format=json&srlimit=3")
	if err != nil { return nil, err }
	defer resp.Body.Close()
	var data map[string]any
	json.NewDecoder(resp.Body).Decode(&data)
	return data, nil
}

// ──────────────────────────────────────────────────────────────────────
// Search Aggregator (parallel multi-engine)
// ──────────────────────────────────────────────────────────────────────

type SearchAggregatorTool struct{}
func (t *SearchAggregatorTool) Name() string { return "search" }
func (t *SearchAggregatorTool) Description() string { return "Search all available engines (DuckDuckGo, Brave, Mojeek, Wikipedia) in parallel and return combined results. Fast — all engines queried simultaneously with 8s timeout each." }
func (t *SearchAggregatorTool) Schema() ToolSchema {
	return ToolSchema{Type:"object", Properties:map[string]PropertyDef{
		"query": {Type:"string", Description:"Search query"},
	}, Required:[]string{"query"}}
}
func (t *SearchAggregatorTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct{ Query string }
	if err := json.Unmarshal(args, &p); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }

	type searchResult struct {
		source string
		data   any
		err    error
	}

	fastClient := &http.Client{Timeout: 8 * time.Second}
	ch := make(chan searchResult, 4)

	// DuckDuckGo — parse results into clean structured format
	go func() {
		ddgCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		form := url.Values{"q": {p.Query}}
		req, _ := http.NewRequestWithContext(ddgCtx, "POST", "https://lite.duckduckgo.com/lite/", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		resp, err := fastClient.Do(req)
		if err != nil { ch <- searchResult{"duckduckgo", nil, err}; return }
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil { ch <- searchResult{"duckduckgo", nil, err}; return }
		results := parseDuckDuckGoResults(string(body), 5)
		ch <- searchResult{"duckduckgo", results, nil}
	}()

	// Brave (if key available)
	go func() {
		apiKey := os.Getenv("BRAVE_SEARCH_API_KEY")
		if apiKey == "" { ch <- searchResult{"brave", nil, fmt.Errorf("no API key set")}; return }
		braveCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(braveCtx, "GET", fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=5", url.QueryEscape(p.Query)), nil)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("X-Subscription-Token", apiKey)
		resp, err := fastClient.Do(req)
		if err != nil { ch <- searchResult{"brave", nil, err}; return }
		defer resp.Body.Close()
		var data map[string]any
		json.NewDecoder(resp.Body).Decode(&data)
		ch <- searchResult{"brave", data, nil}
	}()

	// Mojeek (if key available)
	go func() {
		apiKey := os.Getenv("MOJEEK_API_KEY")
		if apiKey == "" { ch <- searchResult{"mojeek", nil, fmt.Errorf("no API key set")}; return }
		mjCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(mjCtx, "GET", fmt.Sprintf("https://api.mojeek.com/search?q=%s&fmt=json&size=5", url.QueryEscape(p.Query)), nil)
		req.Header.Set("Accept", "application/json")
		resp, err := fastClient.Do(req)
		if err != nil { ch <- searchResult{"mojeek", nil, err}; return }
		defer resp.Body.Close()
		var data map[string]any
		json.NewDecoder(resp.Body).Decode(&data)
		ch <- searchResult{"mojeek", data, nil}
	}()

	// Wikipedia
	go func() {
		wkCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(wkCtx, "GET", "https://en.wikipedia.org/api/rest_v1/page/summary/"+url.PathEscape(p.Query), nil)
		req.Header.Set("User-Agent", "Mozilla/5.0")
		resp, err := fastClient.Do(req)
		if err != nil { ch <- searchResult{"wikipedia", nil, err}; return }
		defer resp.Body.Close()
		var data map[string]any
		json.NewDecoder(resp.Body).Decode(&data)
		ch <- searchResult{"wikipedia", data, nil}
	}()

	results := make(map[string]any)
	for i := 0; i < 4; i++ {
		r := <-ch
		if r.err != nil {
			results[r.source] = map[string]any{"error": r.err.Error()}
		} else {
			results[r.source] = r.data
		}
	}
	return map[string]any{"query": p.Query, "results": results, "engine_count": 4}, nil
}

// parseDuckDuckGoResults extracts structured results from DDG Lite HTML
func parseDuckDuckGoResults(html string, limit int) []map[string]string {
	results := []map[string]string{}
	urlRe := regexp.MustCompile(`class="result-link"[^>]*href="([^"]*)"`)
	urlMatches := urlRe.FindAllStringSubmatch(html, -1)
	if len(urlMatches) == 0 {
		allLinks := regexp.MustCompile(`<a[^>]*href="(https?://[^"]+)"[^>]*>(.*?)</a>`)
		linkMatches := allLinks.FindAllStringSubmatch(html, -1)
		seen := make(map[string]bool)
		for _, m := range linkMatches {
			if len(results) >= limit { break }
			u := cleanURL(m[1])
			if seen[u] || strings.Contains(u, "duckduckgo.com") { continue }
			seen[u] = true
			results = append(results, map[string]string{"title": stripTags(m[2]), "url": u})
		}
		return results
	}
	titleRe := regexp.MustCompile(`class="result-link"[^>]*>(.*?)</a>`)
	titleMatches := titleRe.FindAllStringSubmatch(html, -1)
	snipRe := regexp.MustCompile(`class="result-snippet"[^>]*>(.*?)</td>`)
	snipMatches := snipRe.FindAllStringSubmatch(html, -1)
	for i, m := range urlMatches {
		if len(results) >= limit { break }
		r := map[string]string{"url": cleanURL(m[1])}
		if i < len(titleMatches) { r["title"] = stripTags(titleMatches[i][1]) }
		if i < len(snipMatches) { r["snippet"] = stripTags(snipMatches[i][1]) }
		results = append(results, r)
	}
	return results
}

// ──────────────────────────────────────────────────────────────────────
// Parallel Search (duckduckgo, wikipedia)
// ──────────────────────────────────────────────────────────────────────

type ParallelSearchTool struct{}
func (t *ParallelSearchTool) Name() string { return "parallel_search" }
func (t *ParallelSearchTool) Description() string { return "Search multiple engines (duckduckgo, wikipedia) in parallel and return combined results. Fast — all sources queried simultaneously with 8s timeout each." }
func (t *ParallelSearchTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"query":   {Type: "string", Description: "Search query"},
			"sources": {Type: "string", Description: "Comma-separated sources: duckduckgo,wikipedia,brave,mojeek (default: duckduckgo,wikipedia)"},
		},
		Required: []string{"query"},
	}
}
func (t *ParallelSearchTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Query   string `json:"query"`
		Sources string `json:"sources"`
	}
	if err := json.Unmarshal(args, &p); err != nil { return nil, fmt.Errorf("invalid arguments: %v", err) }
	if p.Sources == "" { p.Sources = "duckduckgo,wikipedia" }

	type result struct {
		source string
		data   any
		err    error
	}
	fastClient := &http.Client{Timeout: 8 * time.Second}
	ch := make(chan result, 8)
	sources := strings.Split(p.Sources, ",")

	for _, src := range sources {
		src = strings.TrimSpace(src)
		go func(s string) {
			srcCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			switch s {
			case "duckduckgo":
				req, _ := http.NewRequestWithContext(srcCtx, "POST", "https://lite.duckduckgo.com/lite/", strings.NewReader(url.Values{"q": {p.Query}}.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
				resp, err := fastClient.Do(req)
				if err != nil { ch <- result{s, nil, err}; return }
				defer resp.Body.Close()
				body, _ := io.ReadAll(resp.Body)
				results := parseDuckDuckGoResults(string(body), 5)
				ch <- result{s, results, nil}
			case "wikipedia":
				req, _ := http.NewRequestWithContext(srcCtx, "GET", "https://en.wikipedia.org/api/rest_v1/page/summary/"+url.PathEscape(p.Query), nil)
				req.Header.Set("User-Agent", "Mozilla/5.0")
				resp, err := fastClient.Do(req)
				if err != nil { ch <- result{s, nil, err}; return }
				defer resp.Body.Close()
				var data map[string]any
				json.NewDecoder(resp.Body).Decode(&data)
				ch <- result{s, data, nil}
			case "brave":
				apiKey := os.Getenv("BRAVE_SEARCH_API_KEY")
				if apiKey == "" { ch <- result{s, nil, fmt.Errorf("no API key")}; return }
				req, _ := http.NewRequestWithContext(srcCtx, "GET", fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=5", url.QueryEscape(p.Query)), nil)
				req.Header.Set("Accept", "application/json")
				req.Header.Set("X-Subscription-Token", apiKey)
				resp, err := fastClient.Do(req)
				if err != nil { ch <- result{s, nil, err}; return }
				defer resp.Body.Close()
				var data map[string]any
				json.NewDecoder(resp.Body).Decode(&data)
				ch <- result{s, data, nil}
			case "mojeek":
				apiKey := os.Getenv("MOJEEK_API_KEY")
				if apiKey == "" { ch <- result{s, nil, fmt.Errorf("no API key")}; return }
				req, _ := http.NewRequestWithContext(srcCtx, "GET", fmt.Sprintf("https://api.mojeek.com/search?q=%s&fmt=json&size=5", url.QueryEscape(p.Query)), nil)
				req.Header.Set("Accept", "application/json")
				resp, err := fastClient.Do(req)
				if err != nil { ch <- result{s, nil, err}; return }
				defer resp.Body.Close()
				var data map[string]any
				json.NewDecoder(resp.Body).Decode(&data)
				ch <- result{s, data, nil}
			default:
				ch <- result{s, nil, fmt.Errorf("unknown source: %s", s)}
			}
		}(src)
	}

	results := make(map[string]any)
	for i := 0; i < len(sources); i++ {
		r := <-ch
		if r.err != nil {
			results[r.source] = map[string]any{"error": r.err.Error()}
		} else {
			results[r.source] = r.data
		}
	}
	return map[string]any{"query": p.Query, "results": results, "sources_searched": len(sources)}, nil
}

func minInt(a, b int) int {
	if a < b { return a }
	return b
}
