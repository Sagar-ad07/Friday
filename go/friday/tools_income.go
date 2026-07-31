package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ── Passive Income Tool ──
//
// Friday uses this to autonomously discover, set up, and manage passive
// income sources. She searches for opportunities, downloads installers,
// configures apps, and monitors earnings — all without human help.
//
// Current supported sources:
//   - Honeygain (bandwidth sharing, $0.5-2/day)
//   - trading bots (already running)
//   - future: microtasks, staking, etc.

type PassiveIncomeTool struct{}

func (t *PassiveIncomeTool) Name() string { return "passive_income" }

func (t *PassiveIncomeTool) Description() string {
	return "DISCOVER and SET UP passive income sources autonomously. Friday uses this to earn money while she runs. Actions: discover (find what's available), setup (configure a source), status (check all income streams), earnings (total daily/monthly estimate). When the user says 'make money' or 'find income' or 'setup earnings', call this."
}

func (t *PassiveIncomeTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]PropertyDef{
			"action": {
				Type: "string",
				Description: "discover (find available income sources), setup (install/configure a source), status (check running), earnings (total estimate)",
				Enum: []string{"discover", "setup", "status", "earnings"},
			},
			"source": {
				Type: "string",
				Description: "For action=setup: which source to set up (honeygain, bandwidth)",
			},
		},
		Required: []string{"action"},
	}
}

type PassiveIncomeStream struct {
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	Status       string  `json:"status"`
	DailyEst     float64 `json:"daily_est_usd"`
	MonthlyEst   float64 `json:"monthly_est_usd"`
	SetupBy      string  `json:"setup_by"`
	Requirements string  `json:"requirements,omitempty"`
}

func (t *PassiveIncomeTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Action string `json:"action"`
		Source string `json:"source"`
	}
	if len(args) > 0 && string(args) != "null" {
		json.Unmarshal(args, &params)
	}
	if params.Action == "" {
		params.Action = "status"
	}

	switch params.Action {
	case "discover":
		return t.discover()
	case "setup":
		return t.setup(params.Source)
	case "status":
		return t.status()
	case "earnings":
		return t.earnings()
	default:
		return map[string]any{"error": "unknown action: " + params.Action}, nil
	}
}

func (t *PassiveIncomeTool) discover() (any, error) {
	sources := []PassiveIncomeStream{
		{
			Name: "Honeygain Bandwidth Sharing", Type: "bandwidth",
			Status: "available", DailyEst: 1.00, MonthlyEst: 30.00,
			Requirements: "Account signup at honeygain.com (email needed), installer downloaded, login required",
		},
		{
			Name: "Exness Trading Bot", Type: "trading",
			Status: "running", DailyEst: 0, MonthlyEst: 0,
			SetupBy: "Friday", Requirements: "Needs winning trades — variable income",
		},
		{
			Name: "BlueGuardian Trading Bot", Type: "trading",
			Status: "running", DailyEst: 0, MonthlyEst: 0,
			SetupBy: "Friday", Requirements: "Needs winning trades — variable income",
		},
		{
			Name: "Binance Grid (paper)", Type: "crypto",
			Status: "inactive", DailyEst: 0, MonthlyEst: 0,
			Requirements: "Needs real Binance API keys + capital to go live",
		},
	}

	// Check Honeygain installation
	if honeygainInstalled() {
		sources[0].Status = "installed"
		sources[0].SetupBy = "pending login"
	}

	return map[string]any{
		"sources": sources,
		"note":    "Trading is the primary engine. Bandwidth sharing is passive background income. Run setup for Honeygain to add $0.5-2/day.",
	}, nil
}

func (t *PassiveIncomeTool) setup(source string) (any, error) {
	switch strings.ToLower(source) {
	case "honeygain", "bandwidth":
		return t.setupHoneygain()
	default:
		return map[string]any{
			"error": fmt.Sprintf("unknown source: %s. Try: honeygain", source),
			"available": []string{"honeygain"},
		}, nil
	}
}

func (t *PassiveIncomeTool) setupHoneygain() (any, error) {
	result := map[string]any{"source": "honeygain"}

	// Check if already installed
	if honeygainInstalled() {
		result["status"] = "already_installed"
		result["note"] = "Honeygain is installed. Login with your account to start earning."
		result["next_step"] = "Open Honeygain from system tray or Start Menu and login. If you don't have an account, sign up at honeygain.com first."
		return result, nil
	}

	// Download installer
	urls := []string{
		"https://download.honeygain.com/agents/HoneygainInstaller.exe",
		"https://honeygain.com/HoneygainInstaller.exe",
	}
	installer := filepath.Join(os.TempDir(), "honeygain_installer.exe")
	downloaded := false

	for _, url := range urls {
		cmd := exec.Command("powershell", "-Command",
			fmt.Sprintf("Invoke-WebRequest -Uri '%s' -OutFile '%s' -UseBasicParsing", url, installer))
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Run(); err == nil {
			if fi, err := os.Stat(installer); err == nil && fi.Size() > 100000 {
				downloaded = true
				result["downloaded_from"] = url
				log.Printf("Honeygain downloaded from %s (%d bytes)", url, fi.Size())
				break
			}
		}
	}

	if !downloaded {
		result["status"] = "download_failed"
		result["note"] = "Could not download Honeygain installer. Please download manually from honeygain.com/download and install it."
		result["manual_url"] = "https://www.honeygain.com/download/"
		return result, nil
	}

	// Install silently
	installCmd := exec.Command(installer, "/S")
	if err := installCmd.Run(); err != nil {
		result["status"] = "install_failed"
		result["error"] = err.Error()
		return result, nil
	}

	time.Sleep(5 * time.Second)
	os.Remove(installer)

	if honeygainInstalled() {
		result["status"] = "installed"
		result["note"] = "Honeygain installed successfully. Now login with your account. If you don't have one, create it at honeygain.com (free)."
		result["next_step"] = "Create account at honeygain.com, then login in the Honeygain app. Expected earnings: $0.5-2/day."
	} else {
		result["status"] = "install_uncertain"
		result["note"] = "Installation completed but app not detected. Check your Start Menu for Honeygain."
	}

	return result, nil
}

func (t *PassiveIncomeTool) status() (any, error) {
	streams := []PassiveIncomeStream{}

	// Trading bots
	streams = append(streams, PassiveIncomeStream{
		Name: "BlueGuardian Trading", Type: "forex",
		Status: "running", DailyEst: 0, MonthlyEst: 0,
		SetupBy: "Friday",
	})
	streams = append(streams, PassiveIncomeStream{
		Name: "Exness Trading", Type: "forex",
		Status: "running", DailyEst: 0, MonthlyEst: 0,
		SetupBy: "Friday",
	})

	// Honeygain
	if honeygainInstalled() {
		streams = append(streams, PassiveIncomeStream{
			Name: "Honeygain Bandwidth", Type: "bandwidth",
			Status: "installed", DailyEst: 1.00, MonthlyEst: 30.00,
			SetupBy: "Friday",
		})
	} else {
		streams = append(streams, PassiveIncomeStream{
			Name: "Honeygain Bandwidth", Type: "bandwidth",
			Status: "not installed", DailyEst: 0, MonthlyEst: 0,
			Requirements: "Call setup with source=honeygain",
		})
	}

	return map[string]any{"streams": streams}, nil
}

func (t *PassiveIncomeTool) earnings() (any, error) {
	// Honeygain process check
	hgRunning := isProcessRunning("honeygain.exe")
	hgDaily := 0.0
	if hgRunning { hgDaily = 1.25 } // average estimate

	// Trading bots
	bgEngine := engineGetOrNil("/mt5/account")
	exEngine := engineGetOrNil("/mt5/exness/account")

	bgBal, bgProfit := 0.0, 0.0
	if bgEngine != nil {
		if bal, ok := bgEngine["balance"].(float64); ok { bgBal = bal }
		if profit, ok := bgEngine["profit"].(float64); ok { bgProfit = profit }
	}

	exBal, exProfit := 0.0, 0.0
	if exEngine != nil {
		if bal, ok := exEngine["balance"].(float64); ok { exBal = bal }
		if profit, ok := exEngine["profit"].(float64); ok { exProfit = profit }
	}

	return map[string]any{
		"streams": []map[string]any{
			{"name": "Honeygain", "status": map[bool]string{true: "running", false: "offline"}[hgRunning],
				"est_daily": hgDaily, "est_monthly": hgDaily * 30, "payout_at": "$20", "type": "passive"},
			{"name": "BlueGuardian", "status": "running", "balance_usd": bgBal, "floating_pnl": bgProfit, "type": "trading"},
			{"name": "Exness", "status": "running", "balance_aed": exBal, "floating_pnl": exProfit, "type": "trading"},
		},
		"total_est_daily": hgDaily,
		"total_est_monthly": hgDaily * 30,
		"next_payout": "$20 from Honeygain (threshold)",
	}, nil
}

func isProcessRunning(name string) bool {
	cmd := exec.Command("tasklist", "/FI", "IMAGENAME eq "+name, "/NH")
	out, err := cmd.Output()
	return err == nil && strings.Contains(strings.ToLower(string(out)), strings.ToLower(name))
}

func honeygainInstalled() bool {
	paths := []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "honeygain"),
		filepath.Join(os.Getenv("APPDATA"), "Honeygain"),
		filepath.Join(os.Getenv("ProgramFiles"), "Honeygain"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Honeygain"),
	}
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			return true
		}
	}
	cmd := exec.Command("tasklist", "/FI", "IMAGENAME eq honeygain.exe", "/NH")
	out, err := cmd.Output()
	return err == nil && strings.Contains(string(out), "honeygain.exe")
}

func engineGetOrNil(path string) map[string]any {
	result, err := engineGet(path)
	if err != nil {
		return nil
	}
	return result
}
