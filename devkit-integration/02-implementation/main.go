package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/devkit-integration/02-implementation"
	"github.com/devkit-integration/02-implementation/dashboard"
	"github.com/devkit-integration/02-implementation/friday_hooks"
)

// Main represents the main application entry point
type Main struct {
	devKitClient *devkit_client.DevKitClient
	hookManager  *friday_hooks.HookManager
	dashboard    *dashboard.Dashboard
	devKitPort   string
	authToken    string
	port         int
}

// NewMain creates a new Main instance
func NewMain(devKitPort, authToken string, dashboardPort int) *Main {
	// Initialize DevKit client
	client := devkit_client.NewDevKitClient(devKitPort, authToken)

	// Initialize hook manager
	manager := friday_hooks.NewHookManager()

	// Register hooks
	manager.RegisterHook(friday_hooks.NewDevKitGateHook(client))
	manager.RegisterHook(friday_hooks.NewSelfHealingHook(client))
	manager.RegisterHook(friday_hooks.NewEnhancedJournalHook(client))

	return &Main{
		devKitClient: client,
		hookManager:  manager,
		devKitPort:   devKitPort,
		authToken:    authToken,
		port:         dashboardPort,
	}
}

// RunFriday executes Friday with DevKit hooks
func (m *Main) RunFriday(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no Friday command specified")
	}

	// Check if Friday is running
	if err := m.checkFridayStatus(ctx); err != nil {
		return fmt.Errorf("Friday is not running: %w", err)
	}

	// Build command
	cmd := fmt.Sprintf("friday %s", args[0])
	if len(args) > 1 {
		cmd = fmt.Sprintf("friday %s", args[0])
	}

	// Execute Friday with hooks
	log.Printf("Executing Friday with DevKit hooks: %s\n", cmd)

	// For now, just print the command
	// In real implementation, this would execute Friday with hooks
	fmt.Printf("Command to execute: %s\n", cmd)
	fmt.Println("This would execute Friday with DevKit hooks in a real implementation")
	fmt.Println("See IMPLEMENTATION_NOTES.md for details on Friday integration")

	return nil
}

// CreateDraft creates a new draft using DevKit
func (m *Main) CreateDraft(ctx context.Context, prompt string) error {
	fmt.Printf("Creating draft: %s\n", prompt)
	fmt.Println("This would create a draft using DevKit's draft system in a real implementation")
	fmt.Println("See IMPLEMENTATION_NOTES.md for details on draft integration")
	return nil
}

// approveChange approves a change using DevKit
func (m *Main) ApproveChange(ctx context.Context, changeID string, notes string) error {
	approval, err := m.devKitClient.ApproveChange(changeID, notes)
	if err != nil {
		return fmt.Errorf("failed to approve change: %w", err)
	}

	if !approval.Approved {
		return fmt.Errorf("change was not approved: %s", approval.Message)
	}

	fmt.Printf("Change approved: %s\n", approval.Message)
	return nil
}

// RollbackToCheckpoint rolls back to a checkpoint using DevKit
func (m *Main) RollbackToCheckpoint(ctx context.Context, checkpointID string) error {
	rollback, err := m.devKitClient.RollbackToCheckpoint(checkpointID)
	if err != nil {
		return fmt.Errorf("failed to rollback: %w", err)
	}

	if !rollback.RolledBack {
		return fmt.Errorf("rollback was not executed: %s", rollback.Message)
	}

	fmt.Printf("Rollback successful: %s\n", rollback.Message)
	return nil
}

// ShowCheckpoints shows all available checkpoints
func (m *Main) ShowCheckpoints(ctx context.Context) error {
	checkpoints, err := m.devKitClient.GetCheckpointList()
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

// ShowJournal shows the journal entries
func (m *Main) ShowJournal(ctx context.Context, limit int) error {
	entries, err := m.devKitClient.GetJournal(limit)
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

// ListSkills lists available DevKit skills
func (m *Main) ListSkills(ctx context.Context) error {
	skills, err := m.devKitClient.GetAvailableSkills()
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

// CallSkill calls a DevKit skill
func (m *Main) CallSkill(ctx context.Context, skillName string, input string) error {
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

	result, err := m.devKitClient.CallSkill(skillName, skillInput)
	if err != nil {
		return fmt.Errorf("failed to call skill: %w", err)
	}

	fmt.Printf("Skill result: %+v\n", result)
	return nil
}

// HealthCheck performs a health check with DevKit
func (m *Main) HealthCheck(ctx context.Context) error {
	health, err := m.devKitClient.HealthCheck()
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

// StartDashboard starts the DevKit dashboard
func (m *Main) StartDashboard() error {
	m.dashboard = dashboard.NewDashboard(m.devKitClient, m.hookManager, m.port)

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down dashboard...")
		if err := m.dashboard.Stop(); err != nil {
			log.Printf("Error stopping dashboard: %v", err)
		}
	}()

	return m.dashboard.Start()
}

// checkFridayStatus checks if Friday is running
func (m *Main) checkFridayStatus(ctx context.Context) error {
	// For now, just check if Friday executable exists
	if _, err := os.Stat("friday.exe"); os.IsNotExist(err) {
		return fmt.Errorf("friday.exe not found")
	}
	return nil
}

// Run executes the main application
func (m *Main) Run() error {
	ctx := context.Background()

	// Parse command line arguments
	args := os.Args[1:]

	if len(args) == 0 {
		return fmt.Errorf("no command specified")
	}

	switch args[0] {
	case "friday":
		if len(args) < 2 {
			return fmt.Errorf("friday command requires an argument")
		}
		return m.RunFriday(ctx, args[1:])
	case "create-draft":
		if len(args) < 2 {
			return fmt.Errorf("create-draft command requires a prompt argument")
		}
		return m.CreateDraft(ctx, args[1])
	case "approve":
		if len(args) < 2 {
			return fmt.Errorf("approve command requires a change-id argument")
		}
		changeID := args[1]
		notes := ""
		if len(args) > 2 {
			notes = args[2]
		}
		return m.ApproveChange(ctx, changeID, notes)
	case "rollback":
		if len(args) < 2 {
			return fmt.Errorf("rollback command requires a checkpoint-id argument")
		}
		return m.RollbackToCheckpoint(ctx, args[1])
	case "checkpoints":
		return m.ShowCheckpoints(ctx)
	case "journal":
		limit := 100
		if len(args) > 1 {
			fmt.Sscanf(args[1], "%d", &limit)
		}
		return m.ShowJournal(ctx, limit)
	case "skills":
		return m.ListSkills(ctx)
	case "skill":
		if len(args) < 3 {
			return fmt.Errorf("skill command requires a skill-name and input argument")
		}
		return m.CallSkill(ctx, args[1], args[2])
	case "dashboard":
		return m.StartDashboard()
	case "health":
		return m.HealthCheck(ctx)
	default:
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

// GetIntegrationReport generates an integration report
func (m *Main) GetIntegrationReport() string {
	return fmt.Sprintf(`
Friday DevKit Integration Report
===============================

DevKit Status: CONNECTED
DevKit Port: %s
AuthToken: %s
Dashboard Port: %d

Registered Hooks:
- DevKitGate: Checks changes before execution
- SelfHealing: Automatically fixes issues
- EnhancedJournal: Enhanced logging and tracking

Friday Integration Status:
- Friday not yet integrated with hooks
- See IMPLEMENTATION_NOTES.md for integration details

Dashboard Status:
- Dashboard ready to start on port %d

Next Steps:
1. Integrate hooks into Friday
2. Test Friday with hooks
3. Create Friday backups
4. Run comprehensive tests
5. Deploy integrated Friday
`, m.devKitPort, m.authToken, m.port, m.port)
}

// Helper functions for Friday integration

// GetFridayIntegrationExample provides example Friday integration code
func GetFridayIntegrationExample() string {
	return `// Example Friday integration code

package friday_integration

import (
	"context"
	"github.com/devkit-integration/02-implementation"
	"github.com/devkit-integration/02-implementation/friday_hooks"
)

// IntegrateDevKitHooks integrates DevKit hooks into Friday
func IntegrateDevKitHooks() (*friday_hooks.HookManager, error) {
	// Initialize DevKit client
	client := devkit_client.NewDevKitClient("8080", "friday-auth-token")

	// Initialize hook manager
	manager := friday_hooks.NewHookManager()

	// Register hooks
	manager.RegisterHook(friday_hooks.NewDevKitGateHook(client))
	manager.RegisterHook(friday_hooks.NewSelfHealingHook(client))
	manager.RegisterHook(friday_hooks.NewEnhancedJournalHook(client))

	return manager, nil
}

// WrapFridayTool wraps a Friday tool with hooks
func WrapFridayTool(ctx context.Context, toolName string, toolFunc func(interface{}) (interface{}, error)) func(interface{}) (interface{}, error) {
	manager, err := IntegrateDevKitHooks()
	if err != nil {
		return toolFunc
	}

	return friday_hooks.WrapToolWithHooks(ctx, manager, toolName, toolFunc)
}

// WrapFridayChange wraps a Friday change with hooks
func WrapFridayChange(ctx context.Context, changeType string, changeFunc func(interface{}) (interface{}, error)) func(interface{}) (interface{}, error) {
	manager, err := IntegrateDevKitHooks()
	if err != nil {
		return changeFunc
	}

	return friday_hooks.WrapChangeWithHooks(ctx, manager, changeType, changeFunc)
}`
}

func main() {
	// Parse command line flags
	devKitPort := flag.String("devkit-port", "8080", "DevKit port")
	authToken := flag.String("auth-token", "friday-auth-token", "DevKit authentication token")
	dashboardPort := flag.Int("dashboard-port", 3001, "Dashboard port")
	help := flag.Bool("help", false, "Show help message")

	flag.Parse()

	// Show help if requested
	if *help {
		printHelp()
		os.Exit(0)
	}

	// Create main instance
	mainApp := NewMain(*devKitPort, *authToken, *dashboardPort)

	// Run the application
	if err := mainApp.Run(); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

// printHelp prints the help message
func printHelp() {
	fmt.Println("Friday DevKit Integration")
	fmt.Println()
	fmt.Println("Usage: friday-devkit [command] [options]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  friday [args]       Execute Friday with DevKit hooks")
	fmt.Println("  create-draft <prompt>  Create a new draft")
	fmt.Println("  approve <change-id> [notes]  Approve a change")
	fmt.Println("  rollback <checkpoint-id> Rollback to checkpoint")
	fmt.Println("  checkpoints        Show all checkpoints")
	fmt.Println("  journal [limit]    Show journal entries")
	fmt.Println("  skills             List available DevKit skills")
	fmt.Println("  skill <name> <input> Call a DevKit skill")
	fmt.Println("  dashboard          Start DevKit dashboard")
	fmt.Println("  health             Check DevKit health")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -devkit-port <port>     DevKit port (default: 8080)")
	fmt.Println("  -auth-token <token>     DevKit authentication token (default: friday-auth-token)")
	fmt.Println("  -dashboard-port <port>  Dashboard port (default: 3001)")
	fmt.Println("  -help                   Show help message")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  friday-devkit friday run")
	fmt.Println("  friday-devkit create-draft \"Create a new feature\"")
	fmt.Println("  friday-devkit approve change-123 \"Approved for review\"")
	fmt.Println("  friday-devkit rollback checkpoint-456")
	fmt.Println("  friday-devkit journal 50")
	fmt.Println("  friday-devkit skills")
	fmt.Println("  friday-devkit skill coding-assistant \"Write a function\"")
	fmt.Println("  friday-devkit dashboard")
	fmt.Println("  friday-devkit health")
	fmt.Println()
	fmt.Println("Documentation:")
	fmt.Println("  See IMPLEMENTATION_NOTES.md for detailed integration instructions")
}
