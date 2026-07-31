package integration_tests

import (
	"context"
	"testing"
	"time"

	"github.com/devkit-integration/02-implementation"
	"github.com/devkit-integration/02-implementation/friday_hooks"
	"github.com/devkit-integration/02-implementation/dashboard"
)

// mockFridayHook is a mock implementation of FridayHook for testing
type mockFridayHook struct {
	name         string
	beforeToolCalled bool
	afterToolCalled bool
	beforeChangeCalled bool
	afterChangeCalled bool
}

func (m *mockFridayHook) Name() string {
	return m.name
}

func (m *mockFridayHook) BeforeTool(ctx context.Context, toolName string, input interface{}) error {
	m.beforeToolCalled = true
	return nil
}

func (m *mockFridayHook) AfterTool(ctx context.Context, toolName string, input interface{}, output interface{}, err error) error {
	m.afterToolCalled = true
	return nil
}

func (m *mockFridayHook) BeforeChange(ctx context.Context, changeType string, changeDetails interface{}) error {
	m.beforeChangeCalled = true
	return nil
}

func (m *mockFridayHook) AfterChange(ctx context.Context, changeType string, changeDetails interface{}, success bool, err error) error {
	m.afterChangeCalled = true
	return nil
}

// TestDevKitClientInitialization tests the DevKit client initialization
func TestDevKitClientInitialization(t *testing.T) {
	client := devkit_client.NewDevKitClient("8080", "test-token")
	if client == nil {
		t.Fatal("Failed to create DevKit client")
	}

	if client.AuthToken != "test-token" {
		t.Errorf("Expected auth token 'test-token', got '%s'", client.AuthToken)
	}

	if client.DevKitPort != "8080" {
		t.Errorf("Expected port '8080', got '%s'", client.DevKitPort)
	}
}

// TestHookManagerInitialization tests the hook manager initialization
func TestHookManagerInitialization(t *testing.T) {
	manager := friday_hooks.NewHookManager()
	if manager == nil {
		t.Fatal("Failed to create hook manager")
	}

	if len(manager.hooks) != 0 {
		t.Errorf("Expected 0 hooks, got %d", len(manager.hooks))
	}
}

// TestHookRegistration tests hook registration
func TestHookRegistration(t *testing.T) {
	manager := friday_hooks.NewHookManager()

	// Create a mock hook
	mockHook := &mockFridayHook{name: "mock"}

	// Register hook
	manager.RegisterHook(mockHook)

	if len(manager.hooks) != 1 {
		t.Errorf("Expected 1 hook, got %d", len(manager.hooks))
	}

	// Unregister hook
	manager.UnregisterHook("mock")

	if len(manager.hooks) != 0 {
		t.Errorf("Expected 0 hooks after unregister, got %d", len(manager.hooks))
	}
}

// TestDevKitGateHook tests the DevKit gate hook
func TestDevKitGateHook(t *testing.T) {
	client := devkit_client.NewDevKitClient("8080", "test-token")
	hook := friday_hooks.NewDevKitGateHook(client)

	if hook == nil {
		t.Fatal("Failed to create DevKit gate hook")
	}

	if hook.Name() != "DevKitGate" {
		t.Errorf("Expected hook name 'DevKitGate', got '%s'", hook.Name())
	}
}

// TestSelfHealingHook tests the self-healing hook
func TestSelfHealingHook(t *testing.T) {
	client := devkit_client.NewDevKitClient("8080", "test-token")
	hook := friday_hooks.NewSelfHealingHook(client)

	if hook == nil {
		t.Fatal("Failed to create self-healing hook")
	}

	if hook.Name() != "SelfHealing" {
		t.Errorf("Expected hook name 'SelfHealing', got '%s'", hook.Name())
	}
}

// TestEnhancedJournalHook tests the enhanced journal hook
func TestEnhancedJournalHook(t *testing.T) {
	client := devkit_client.NewDevKitClient("8080", "test-token")
	hook := friday_hooks.NewEnhancedJournalHook(client)

	if hook == nil {
		t.Fatal("Failed to create enhanced journal hook")
	}

	if hook.Name() != "EnhancedJournal" {
		t.Errorf("Expected hook name 'EnhancedJournal', got '%s'", hook.Name())
	}
}

// TestExecuteIntegration tests the integration execution
func TestExecuteIntegration(t *testing.T) {
	client := devkit_client.NewDevKitClient("8080", "test-token")

	// Define a simple operation
	operation := func() (interface{}, error) {
		return "operation completed", nil
	}

	result, err := devkit_client.ExecuteIntegration(client, "test operation", "test_type", operation)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if !result.Success {
		t.Error("Expected success to be true")
	}

	if result.Message != "Operation completed successfully" {
		t.Errorf("Expected 'Operation completed successfully', got '%s'", result.Message)
	}

	if result.ChangeID == "" {
		t.Error("Expected change ID to be set")
	}
}

// TestMustBeApproved tests the approval check
func TestMustBeApproved(t *testing.T) {
	client := devkit_client.NewDevKitClient("8080", "test-token")

	approved, err := devkit_client.MustBeApproved(client, "test description", "test_type")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if !approved {
		t.Error("Expected approval to be true")
	}
}

// TestCheckBeforeApprove tests the approval check with change ID
func TestCheckBeforeApprove(t *testing.T) {
	client := devkit_client.NewDevKitClient("8080", "test-token")

	changeID, err := devkit_client.CheckBeforeApprove(client, "test description", "test_type")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if changeID == "" {
		t.Error("Expected change ID to be set")
	}
}

// TestGetFridayTools tests getting Friday tools
func TestGetFridayTools(t *testing.T) {
	tools := devkit_client.GetFridayTools()
	if len(tools) == 0 {
		t.Error("Expected at least one Friday tool")
	}

	// Check for some known tools
	hasWeather := false
	hasCalendar := false
	for _, tool := range tools {
		if tool == "weather" {
			hasWeather = true
		}
		if tool == "calendar" {
			hasCalendar = true
		}
	}

	if !hasWeather {
		t.Error("Expected 'weather' tool to be available")
	}

	if !hasCalendar {
		t.Error("Expected 'calendar' tool to be available")
	}
}

// TestDashboardInitialization tests dashboard initialization
func TestDashboardInitialization(t *testing.T) {
	client := &devkit_client.DevKitClient{}
	hookManager := &friday_hooks.HookManager{}

	dashboard := dashboard.NewDashboard(client, hookManager, 3001)
	if dashboard == nil {
		t.Fatal("Failed to create dashboard")
	}

	if dashboard.server.Addr != ":3001" {
		t.Errorf("Expected address ':3001', got '%s'", dashboard.server.Addr)
	}
}

// TestDashboardChannels tests channel management
func TestDashboardChannels(t *testing.T) {
	client := &devkit_client.DevKitClient{}
	hookManager := &friday_hooks.HookManager{}
	dashboard := dashboard.NewDashboard(client, hookManager, 3001)

	// Add channel
	ch := dashboard.AddChannel("test_channel")
	if ch == nil {
		t.Fatal("Failed to add channel")
	}

	// Check channel exists
	dashboard.mu.RLock()
	_, exists := dashboard.channels["test_channel"]
	dashboard.mu.RUnlock()

	if !exists {
		t.Error("Expected channel to exist after adding")
	}

	// Remove channel
	dashboard.RemoveChannel("test_channel")

	// Check channel removed
	dashboard.mu.RLock()
	_, exists = dashboard.channels["test_channel"]
	dashboard.mu.RUnlock()

	if exists {
		t.Error("Expected channel to be removed")
	}
}

// TestBroadcast tests broadcast functionality
func TestBroadcast(t *testing.T) {
	client := &devkit_client.DevKitClient{}
	hookManager := &friday_hooks.HookManager{}
	dashboard := dashboard.NewDashboard(client, hookManager, 3001)

	ch := dashboard.AddChannel("test_channel")
	if ch == nil {
		t.Fatal("Failed to add channel")
	}

	// Try to broadcast
	err := dashboard.Broadcast("test_channel", map[string]string{"test": "data"})
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

// TestExecuteBeforeTool tests tool execution with hooks
func TestExecuteBeforeTool(t *testing.T) {
	manager := friday_hooks.NewHookManager()

	// Execute before hook (should be no-op for mock hook)
	err := manager.ExecuteBeforeTool(context.Background(), "test_tool", nil)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

// TestExecuteAfterTool tests tool execution with hooks
func TestExecuteAfterTool(t *testing.T) {
	manager := friday_hooks.NewHookManager()

	// Execute after hook (should be no-op for mock hook)
	err := manager.ExecuteAfterTool(context.Background(), "test_tool", nil, nil, nil)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

// TestExecuteBeforeChange tests change execution with hooks
func TestExecuteBeforeChange(t *testing.T) {
	manager := friday_hooks.NewHookManager()

	// Execute before change hook (should be no-op for mock hook)
	err := manager.ExecuteBeforeChange(context.Background(), "test_change", nil)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

// TestExecuteAfterChange tests change execution with hooks
func TestExecuteAfterChange(t *testing.T) {
	manager := friday_hooks.NewHookManager()

	// Execute after change hook (should be no-op for mock hook)
	err := manager.ExecuteAfterChange(context.Background(), "test_change", nil, true, nil)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

// TestWrapToolWithHooks tests wrapping tools with hooks
func TestWrapToolWithHooks(t *testing.T) {
	manager := friday_hooks.NewHookManager()

	// Define a simple tool function
	toolFunc := func(input interface{}) (interface{}, error) {
		return input, nil
	}

	// Wrap with hooks
	wrappedTool := friday_hooks.WrapToolWithHooks(context.Background(), manager, "test_tool", toolFunc)

	// Execute wrapped tool
	result, err := wrappedTool("test input")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if result != "test input" {
		t.Errorf("Expected 'test input', got '%v'", result)
	}
}

// TestWrapChangeWithHooks tests wrapping changes with hooks
func TestWrapChangeWithHooks(t *testing.T) {
	manager := friday_hooks.NewHookManager()

	// Define a simple change function
	changeFunc := func(changeDetails interface{}) (interface{}, error) {
		return changeDetails, nil
	}

	// Wrap with hooks
	wrappedChange := friday_hooks.WrapChangeWithHooks(context.Background(), manager, "test_change", changeFunc)

	// Execute wrapped change
	result, err := wrappedChange(map[string]string{"key": "value"})
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}
}

// TestExecuteWithCheckpoint tests executing with a DevKit checkpoint
func TestExecuteWithCheckpoint(t *testing.T) {
	client := devkit_client.NewDevKitClient("8080", "test-token")

	// Define a simple operation
	operation := func() (interface{}, error) {
		return "operation completed", nil
	}

	// Execute with checkpoint
	result, err := friday_hooks.ExecuteWithCheckpoint(context.Background(), client, "test checkpoint", operation)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if result != "operation completed" {
		t.Errorf("Expected 'operation completed', got '%v'", result)
	}
}

// TestDashboardNotifyChange tests change notification
func TestDashboardNotifyChange(t *testing.T) {
	client := &devkit_client.DevKitClient{}
	hookManager := &friday_hooks.HookManager{}
	dashboard := dashboard.NewDashboard(client, hookManager, 3001)

	// Add channel
	ch := dashboard.AddChannel("test_channel")
	if ch == nil {
		t.Fatal("Failed to add channel")
	}

	// Notify change
	err := dashboard.NotifyChange("test_change", map[string]string{"key": "value"})
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

// TestDashboardNotifyCheckpoint tests checkpoint notification
func TestDashboardNotifyCheckpoint(t *testing.T) {
	client := &devkit_client.DevKitClient{}
	hookManager := &friday_hooks.HookManager{}
	dashboard := dashboard.NewDashboard(client, hookManager, 3001)

	// Add channel
	ch := dashboard.AddChannel("test_channel")
	if ch == nil {
		t.Fatal("Failed to add channel")
	}

	// Notify checkpoint
	checkpoint := &devkit_client.CheckpointInfo{
		ID:          "test_id",
		Description: "test checkpoint",
		Timestamp:   time.Now().Unix(),
		CreatedBy:   "test_user",
	}

	err := dashboard.NotifyCheckpoint(checkpoint)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

// TestDashboardNotifyJournalEntry tests journal entry notification
func TestDashboardNotifyJournalEntry(t *testing.T) {
	client := &devkit_client.DevKitClient{}
	hookManager := &friday_hooks.HookManager{}
	dashboard := dashboard.NewDashboard(client, hookManager, 3001)

	// Add channel
	ch := dashboard.AddChannel("test_channel")
	if ch == nil {
		t.Fatal("Failed to add channel")
	}

	// Notify journal entry
	entry := &devkit_client.JournalEntry{
		ID:          "test_id",
		Type:        "test_type",
		Data:        map[string]interface{}{"key": "value"},
		Timestamp:   time.Now().Unix(),
		CreatedBy:   "test_user",
		Message:     "test message",
	}

	err := dashboard.NotifyJournalEntry(entry)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

// RunIntegrationTests runs all integration tests
func RunIntegrationTests(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*testing.T)
	}{
		{"TestDevKitClientInitialization", TestDevKitClientInitialization},
		{"TestHookManagerInitialization", TestHookManagerInitialization},
		{"TestHookRegistration", TestHookRegistration},
		{"TestDevKitGateHook", TestDevKitGateHook},
		{"TestSelfHealingHook", TestSelfHealingHook},
		{"TestEnhancedJournalHook", TestEnhancedJournalHook},
		{"TestExecuteIntegration", TestExecuteIntegration},
		{"TestMustBeApproved", TestMustBeApproved},
		{"TestCheckBeforeApprove", TestCheckBeforeApprove},
		{"TestGetFridayTools", TestGetFridayTools},
		{"TestDashboardInitialization", TestDashboardInitialization},
		{"TestDashboardChannels", TestDashboardChannels},
		{"TestBroadcast", TestBroadcast},
		{"TestExecuteBeforeTool", TestExecuteBeforeTool},
		{"TestExecuteAfterTool", TestExecuteAfterTool},
		{"TestExecuteBeforeChange", TestExecuteBeforeChange},
		{"TestExecuteAfterChange", TestExecuteAfterChange},
		{"TestWrapToolWithHooks", TestWrapToolWithHooks},
		{"TestWrapChangeWithHooks", TestWrapChangeWithHooks},
		{"TestExecuteWithCheckpoint", TestExecuteWithCheckpoint},
		{"TestDashboardNotifyChange", TestDashboardNotifyChange},
		{"TestDashboardNotifyCheckpoint", TestDashboardNotifyCheckpoint},
		{"TestDashboardNotifyJournalEntry", TestDashboardNotifyJournalEntry},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.fn)
	}
}

// Integration test configuration
type IntegrationTestConfig struct {
	DevKitPort     string
	AuthToken      string
	UseDevKit      bool
	Verbose        bool
	Timeout        time.Duration
	Reporters      []string
	Parallel       bool
	StopOnFailure  bool
	FailFast       bool
}

// Default integration test configuration
func DefaultIntegrationTestConfig() *IntegrationTestConfig {
	return &IntegrationTestConfig{
		DevKitPort:     "8080",
		AuthToken:      "friday-auth-token",
		UseDevKit:      true,
		Verbose:        false,
		Timeout:        30 * time.Second,
		Reporters:      []string{"text"},
		Parallel:       false,
		StopOnFailure:  true,
		FailFast:       true,
	}
}

// RunTestsWithConfig runs integration tests with the specified configuration
func RunTestsWithConfig(config *IntegrationTestConfig) error {
	t := &testing.T{}
	RunIntegrationTests(t)

	// In real implementation, this would run with the specified configuration
	return nil
}
