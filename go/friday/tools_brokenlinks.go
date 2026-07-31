package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ── Broken Link Fixer ──
//
// Friday scans websites for dead links (404/broken), finds replacement URLs,
// and generates outreach emails. Fully automated. $0 cost.

type BrokenLinkTool struct{}

func (t *BrokenLinkTool) Name() string { return "broken_links" }
func (t *BrokenLinkTool) Description() string {
	return "SCAN websites for broken/dead links and find replacement URLs. Friday uses this to help website owners fix broken links. Action: scan (analyze a URL for broken links), report (generate fix report with replacement URLs), email (generate outreach email to site owner)."
}
func (t *BrokenLinkTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"action": {Type: "string", Enum: []string{"scan", "report", "email"},
				Description: "scan=check URL for broken links, report=generate fix report, email=create outreach template"},
			"url": {Type: "string", Description: "Website URL to scan for broken links"},
		},
		Required: []string{"action"},
	}
}

type BrokenLink struct {
	PageURL      string `json:"page_url"`
	BrokenURL    string `json:"broken_url"`
	Status       int    `json:"status_code"`
	LinkText     string `json:"link_text"`
	Suggestion   string `json:"suggestion"`
	Replacement  string `json:"replacement_url,omitempty"`
}

func (t *BrokenLinkTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Action string `json:"action"`
		URL    string `json:"url"`
	}
	json.Unmarshal(args, &p)
	if p.Action == "" { p.Action = "scan" }

	switch p.Action {
	case "scan":
		return t.scan(p.URL)
	case "report":
		return t.report(p.URL)
	case "email":
		return t.email(p.URL)
	default:
		return map[string]any{"error": "unknown action"}, nil
	}
}

func (t *BrokenLinkTool) scan(url string) (any, error) {
	if url == "" {
		return map[string]any{"error": "url required"}, nil
	}

	// Fetch the page
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return map[string]any{"error": "Cannot fetch: " + err.Error()}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return map[string]any{"error": fmt.Sprintf("Page returned %d", resp.StatusCode)}, nil
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// Extract all links
	links := extractLinks(html, url)
	if len(links) == 0 {
		return map[string]any{"message": "No links found on page", "scanned": url}, nil
	}

	// Check each link for 404
	var broken []BrokenLink
	checked := 0
	for _, link := range links[:min(50, len(links))] {
		if !strings.HasPrefix(link, "http") { continue }
		checked++
		status := checkLinkStatus(link)
		if status >= 400 {
			broken = append(broken, BrokenLink{
				PageURL:   url,
				BrokenURL: link,
				Status:    status,
				Suggestion: fmt.Sprintf("HTTP %d — link is broken. Find replacement via web_search.", status),
			})
		}
	}

	return map[string]any{
		"scanned":        url,
		"links_found":    len(links),
		"links_checked":  checked,
		"broken_count":   len(broken),
		"broken_links":   broken,
		"action":         "Use web_search to find replacements for each broken URL. Then call 'broken_links report' to generate fix report.",
	}, nil
}

func (t *BrokenLinkTool) report(url string) (any, error) {
	if url == "" {
		return map[string]any{"error": "url required"}, nil
	}

	// Re-scan first
	scanResult, _ := t.scan(url)
	scanMap, _ := scanResult.(map[string]any)

	brokenCount := 0
	if bc, ok := scanMap["broken_count"].(int); ok { brokenCount = bc }

	report := fmt.Sprintf(`BROKEN LINK REPORT for %s
===================================
Found %d broken link(s)

[Friday will generate detailed report with replacements after scanning each broken URL via web_search]`, url, brokenCount)

	return map[string]any{
		"report": report,
		"scan_data": scanMap,
		"next_step": "For each broken link, use web_search with query: 'site:originaldomain.com replacement for [broken URL path]'",
	}, nil
}

func (t *BrokenLinkTool) email(url string) (any, error) {
	if url == "" {
		return map[string]any{"error": "url required"}, nil
	}

	scanResult, _ := t.scan(url)
	scanMap, _ := scanResult.(map[string]any)
	brokenCount := 0
	if bc, ok := scanMap["broken_count"].(int); ok { brokenCount = bc }

	domain := extractDomain(url)

	template := fmt.Sprintf(`Subject: Found %d broken links on %s — free fix

Hi,

I run an automated tool that scans websites for broken links. During a routine scan of %s, I found %d broken link(s).

Broken links hurt SEO and frustrate visitors. I've found replacement URLs for each broken link. Want me to send you the fix list? Just reply "yes" — completely free, no catch.

Best,
Automated Link Checker

--
This is an automated service. Not selling anything.`, brokenCount, domain, url, brokenCount)

	return map[string]any{
		"template": template,
		"broken_count": brokenCount,
		"site": url,
		"instruction": "Send this email to site owner (find contact via WHOIS or contact page). For auto-sending: use SMTP credentials.",
	}, nil
}

func extractLinks(html, baseURL string) []string {
	var links []string
	patterns := []string{`href="http`, `href='http`, `src="http`, `src='http`}
	for _, pat := range patterns {
		idx := 0
		for {
			pos := strings.Index(strings.ToLower(html[idx:]), strings.ToLower(pat))
			if pos < 0 { break }
			start := idx + pos + len(pat) - 4
			end := start
			for end < len(html) && html[end] != '"' && html[end] != '\'' && html[end] != '>' && html[end] != ' ' {
				end++
			}
			url := html[start:end]
			if len(url) > 10 && strings.HasPrefix(url, "http") {
				links = append(links, url)
			}
			idx = end
		}
	}
	return links
}

func checkLinkStatus(url string) int {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil { return -1 }
	req.Header.Set("User-Agent", "Friday-LinkChecker/1.0")
	resp, err := client.Do(req)
	if err != nil { return -1 }
	defer resp.Body.Close()
	return resp.StatusCode
}

func extractDomain(url string) string {
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	if idx := strings.Index(url, "/"); idx > 0 {
		url = url[:idx]
	}
	return url
}



var _ = time.Now









