package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/devkit-integration/02-implementation"
)

// CLI represents the DevKit CLI wrapper
type CLI struct {
	devKitClient *devkit_client.DevKitClient
	hookManager  *friday_hooks.HookManager
}

// NewCLI creates a new CLI instance
func NewCLI(devKitPort, authToken string) *CLI {
	client := devkit_client.NewDevKitClient(devKitPort, authToken)
	hookManager := friday_hooks.NewHookManager()

	// Register hooks
	hookManager.RegisterHook(friday_hooks.NewDevKitGateHook(client))
	hookManager.RegisterHook(friday_hooks.NewSelfHealingHook(client))
	hookManager.RegisterHook(friday_hooks.NewEnhancedJournalHook(client))

	return &CLI{
		devKitClient: client,
		hookManager:  hookManager,
	}
}

// runFriday executes Friday with hooks
func (c *CLI) runFriday(ctx context.Context, args []string) error {
	// Check if Friday is running
	if err := c.checkFridayStatus(ctx); err != nil {
		return fmt.Errorf("Friday is not running: %w", err)
	}

	// Execute Friday with hooks
	cmd := exec.Command("friday", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Friday execution failed: %w", err)
	}

	return nil
}

// createDraft creates a new draft using DevKit
func (c *CLI) createDraft(ctx context.Context, prompt string) error {
	if c.devKitClient == nil {
		return fmt.Errorf("DevKit client not configured")
	}

	// Log draft creation
	if err := c.hookManager.ExecuteBeforeChange(ctx, "draft", map[string]interface{}{
		"prompt": prompt,
		"type":   "create",
	}); err != nil {
		return fmt.Errorf("failed to execute before change hook: %w", err)
	}

	// Use DevKit's draft system
	// This would call DevKit's API to create a draft
	fmt.Println("Creating draft with DevKit...")
	fmt.Printf("Prompt: %s\n", prompt)

	// Create a temporary file for the draft
	draftContent := fmt.Sprintf("# Draft\n\n%s", prompt)
	draftPath := filepath.Join("drafts", fmt.Sprintf("draft_%d.md", time.Now().Unix()))

	// Create drafts directory if it doesn't exist
	if err := os.MkdirAll("drafts", 0755); err != nil {
		return fmt.Errorf("failed to create drafts directory: %w", err)
	}

	if err := os.WriteFile(draftPath, []byte(draftContent), 0644); err != nil {
		return fmt.Errorf("failed to create draft file: %w", err)
	}

	fmt.Printf("Draft created: %s\n", draftPath)

	// Execute after change hook
	if err := c.hookManager.ExecuteAfterChange(ctx, "draft", map[string]interface{}{
		"prompt": prompt,
		"success": true,
		"file":    draftPath,
	}); err != nil {
		return fmt.Errorf("failed to execute after change hook: %w", err)
	}

	return nil
}

// approveChange approves a change using DevKit
func (c *CLI) approveChange(ctx context.Context, changeID string, notes string) error {
	if c.devKitClient == nil {
		return fmt.Errorf("DevKit client not configured")
	}

	approval, err := c.devKitClient.ApproveChange(changeID, notes)
	if err != nil {
		return fmt.Errorf("failed to approve change: %w", err)
	}

	if !approval.Approved {
		return fmt.Errorf("change was not approved: %s", approval.Message)
	}

	fmt.Printf("Change approved: %s\n", approval.Message)
	return nil
}

// rollbackToCheckpoint rolls back to a checkpoint using DevKit
func (c *CLI) rollbackToCheckpoint(ctx context.Context, checkpointID string) error {
	if c.devKitClient == nil {
		return fmt.Errorf("DevKit client not configured")
	}

	rollback, err := c.devKitClient.RollbackToCheckpoint(checkpointID)
	if err != nil {
		return fmt.Errorf("failed to rollback: %w", err)
	}

	if !rollback.RolledBack {
		return fmt.Errorf("rollback was not executed: %s", rollback.Message)
	}

	fmt.Printf("Rollback successful: %s\n", rollback.Message)
	return nil
}

// showCheckpoints shows all available checkpoints
func (c *CLI) showCheckpoints(ctx context.Context) error {
	if c.devKitClient == nil {
		return fmt.Errorf("DevKit client not configured")
	}

	checkpoints, err := c.devKitClient.GetCheckpointList()
	if err != nil {
		return fmt.Errorf("failed to get checkpoints: %w", err)
	}

	fmt.Println("Available Checkpoints:")
	fmt.Println("=====================")
	for _, cp := range checkpoints {
		fmt.Printf("ID: %s\n", cp.ID)
		fmt.Printf("Description: %s\n", cp.Description)
		fmt.Printf("Timestamp: %d\n", cp.Timestamp)
		fmt.Printf("Created by: %s\n", cp.CreatedBy)
		fmt.Println()
	}

	return nil
}

// showJournal shows the journal entries
func (c *CLI) showJournal(ctx context.Context, limit int) error {
	if c.devKitClient == nil {
		return fmt.Errorf("DevKit client not configured")
	}

	entries, err := c.devKitClient.GetJournal(limit)
	if err != nil {
		return fmt.Errorf("failed to get journal entries: %w", err)
	}

	fmt.Println("Journal Entries:")
	fmt.Println("================")
	for _, entry := range entries {
		fmt.Printf("Type: %s\n", entry.Type)
		fmt.Printf("Timestamp: %d\n", entry.Timestamp)
		fmt.Printf("Created by: %s\n", entry.CreatedBy)
		fmt.Printf("Message: %s\n", entry.Message)
		fmt.Println()
	}

	return nil
}

// listSkills lists available DevKit skills
func (c *CLI) listSkills(ctx context.Context) error {
	if c.devKitClient == nil {
		return fmt.Errorf("DevKit client not configured")
	}

	skills, err := c.devKitClient.GetAvailableSkills()
	if err != nil {
		return fmt.Errorf("failed to get skills: %w", err)
	}

	fmt.Println("Available DevKit Skills:")
	fmt.Println("========================")
	for _, skill := range skills {
		fmt.Printf("Name: %s\n", skill.Name)
		fmt.Printf("Description: %s\n", skill.Description)
		fmt.Println()
	}

	return nil
}

// callSkill calls a DevKit skill
func (c *CLI) callSkill(ctx context.Context, skillName string, input string) error {
	if c.devKitClient == nil {
		return fmt.Errorf("DevKit client not configured")
	}

	// Parse input if it's JSON
	var inputData map[string]interface{}
	if err := json.Unmarshal([]byte(input), &inputData); err != nil {
		inputData = map[string]interface{}{
			"input": input,
		}
	}

	skillInput := map[string]interface{}{
		"skill_name": skillName,
		"input":      inputData,
	}

	result, err := c.devKitClient.CallSkill(skillName, skillInput)
	if err != nil {
		return fmt.Errorf("failed to call skill: %w", err)
	}

	fmt.Printf("Skill result: %+v\n", result)
	return nil
}

// healthCheck performs a health check with DevKit
func (c *CLI) healthCheck(ctx context.Context) error {
	if c.devKitClient == nil {
		return fmt.Errorf("DevKit client not configured")
	}

	health, err := c.devKitClient.HealthCheck()
	if err != nil {
		return fmt.Errorf("failed to perform health check: %w", err)
	}

	fmt.Printf("DevKit Status: %s\n", health.Status)
	fmt.Printf("Message: %s\n", health.Message)
	if health.Version != "" {
		fmt.Printf("Version: %s\n", health.Version)
	}
	if health.Uptime > 0 {
		fmt.Printf("Uptime: %d seconds\n", health.Uptime)
	}

	return nil
}

// main is the entry point for the DevKit CLI
func main() {
	// Parse command line flags
	port := flag.String("port", "8080", "DevKit port")
	auth := flag.String("auth", "friday-auth-token", "DevKit authentication token")
	help := flag.Bool("help", false, "Show help message")

	flag.Parse()

	// Show help if requested
	if *help {
		printHelp()
		os.Exit(0)
	}

	// Create CLI instance
	cli := NewCLI(*port, *auth)

	// Check if there are arguments after flags
	args := flag.Args()
	if len(args) == 0 {
		printHelp()
		os.Exit(1)
	}

	// Execute command
	ctx := context.Background()
	switch args[0] {
	case "friday":
		if err := cli.runFriday(ctx, args[1:]); err != nil {
			log.Fatalf("Friday execution failed: %v", err)
		}
	case "create-draft":
		if len(args) < 2 {
			log.Fatal("Usage: devkit-cli create-draft <prompt>")
		}
		if err := cli.createDraft(ctx, args[1]); err != nil {
			log.Fatalf("Failed to create draft: %v", err)
		}
	case "approve":
		if len(args) < 2 {
			log.Fatal("Usage: devkit-cli approve <change-id> [notes]")
		}
		changeID := args[1]
		notes := ""
		if len(args) > 2 {
			notes = args[2]
		}
		if err := cli.approveChange(ctx, changeID, notes); err != nil {
			log.Fatalf("Failed to approve change: %v", err)
		}
	case "rollback":
		if len(args) < 2 {
			log.Fatal("Usage: devkit-cli rollback <checkpoint-id>")
		}
		if err := cli.rollbackToCheckpoint(ctx, args[1]); err != nil {
			log.Fatalf("Failed to rollback: %v", err)
		}
	case "checkpoints":
		if err := cli.showCheckpoints(ctx); err != nil {
			log.Fatalf("Failed to show checkpoints: %v", err)
		}
	case "journal":
		limit := 100
		if len(args) > 1 {
			if _, err := fmt.Sscanf(args[1], "%d", &limit); err != nil {
				log.Fatal("Invalid limit value")
			}
		}
		if err := cli.showJournal(ctx, limit); err != nil {
			log.Fatalf("Failed to show journal: %v", err)
		}
	case "skills":
		if err := cli.listSkills(ctx); err != nil {
			log.Fatalf("Failed to list skills: %v", err)
		}
	case "skill":
		if len(args) < 3 {
			log.Fatal("Usage: devkit-cli skill <skill-name> <input>")
		}
		skillName := args[1]
		input := args[2]
		if err := cli.callSkill(ctx, skillName, input); err != nil {
			log.Fatalf("Failed to call skill: %v", err)
		}
	case "health":
		if err := cli.healthCheck(ctx); err != nil {
			log.Fatalf("Failed health check: %v", err)
		}
	default:
		log.Printf("Unknown command: %s\n", args[0])
		printHelp()
		os.Exit(1)
	}
}

// printHelp prints the help message
func printHelp() {
	fmt.Println("DevKit CLI - Friday Development Wrapper")
	fmt.Println()
	fmt.Println("Usage: devkit-cli [command] [options]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  friday [args]      Execute Friday with DevKit hooks")
	fmt.Println("  create-draft <prompt>  Create a new draft")
	fmt.Println("  approve <change-id> [notes]  Approve a change")
	fmt.Println("  rollback <checkpoint-id> Rollback to checkpoint")
	fmt.Println("  checkpoints        Show all checkpoints")
	fmt.Println("  journal [limit]    Show journal entries")
	fmt.Println("  skills             List available DevKit skills")
	fmt.Println("  skill <name> <input> Call a DevKit skill")
	fmt.Println("  health             Check DevKit health")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -port <port>       DevKit port (default: 8080)")
	fmt.Println("  -auth <token>      DevKit authentication token (default: friday-auth-token)")
	fmt.Println("  -help              Show help message")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  devkit-cli friday run")
	fmt.Println("  devkit-cli create-draft \"Create a new feature\"")
	fmt.Println("  devkit-cli approve change-123 \"Approved for review\"")
	fmt.Println("  devkit-cli rollback checkpoint-456")
	fmt.Println("  devkit-cli journal 50")
	fmt.Println("  devkit-cli skills")
	fmt.Println("  devkit-cli skill coding-assistant \"Write a function\"")
	fmt.Println("  devkit-cli health")
}
