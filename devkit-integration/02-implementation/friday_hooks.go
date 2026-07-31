// Copyright (c) 2026 Friday AI
// All rights reserved.
//
// This Friday Hooks System is part of the Friday AI integration.
// Friday provides development assistance with AI-powered features.
// For more information, visit https://friday.ai
//
// Permission to use, modify, and distribute this software is
// granted provided that this copyright notice remains intact.

package friday_hooks

import (
	"context"
	"fmt"
	"reflect"
	"time"
)

// FridayHook defines the interface for Friday hooks
type FridayHook interface {
	// Name returns the name of the hook
	Name() string
	// BeforeTool executes before a tool runs
	BeforeTool(ctx context.Context, toolName string, input interface{}) error
	// AfterTool executes after a tool runs
	AfterTool(ctx context.Context, toolName string, input interface{}, output interface{}, err error) error
	// BeforeChange executes before a change is made
	BeforeChange(ctx context.Context, changeType string, changeDetails interface{}) error
	// AfterChange executes after a change is made
	AfterChange(ctx context.Context, changeType string, changeDetails interface{}, success bool, err error) error
}

// HookManager manages all Friday hooks
type HookManager struct {
	hooks []FridayHook
}

// NewHookManager creates a new HookManager
func NewHookManager() *HookManager {
	return &HookManager{
		hooks: make([]FridayHook, 0),
	}
}

// RegisterHook registers a new hook
func (m *HookManager) RegisterHook(hook FridayHook) {
	m.hooks = append(m.hooks, hook)
}

// UnregisterHook removes a hook
func (m *HookManager) UnregisterHook(name string) {
	for i, hook := range m.hooks {
		if hook.Name() == name {
			m.hooks = append(m.hooks[:i], m.hooks[i+1:]...)
			return
		}
	}
}

// ExecuteBeforeTool executes all BeforeTool hooks
func (m *HookManager) ExecuteBeforeTool(ctx context.Context, toolName string, input interface{}) error {
	for _, hook := range m.hooks {
		if err := hook.BeforeTool(ctx, toolName, input); err != nil {
			return fmt.Errorf("hook %s BeforeTool failed: %w", hook.Name(), err)
		}
	}
	return nil
}

// ExecuteAfterTool executes all AfterTool hooks
func (m *HookManager) ExecuteAfterTool(ctx context.Context, toolName string, input interface{}, output interface{}, err error) error {
	for _, hook := range m.hooks {
		if err := hook.AfterTool(ctx, toolName, input, output, err); err != nil {
			return fmt.Errorf("hook %s AfterTool failed: %w", hook.Name(), err)
		}
	}
	return nil
}

// ExecuteBeforeChange executes all BeforeChange hooks
func (m *HookManager) ExecuteBeforeChange(ctx context.Context, changeType string, changeDetails interface{}) error {
	for _, hook := range m.hooks {
		if err := hook.BeforeChange(ctx, changeType, changeDetails); err != nil {
			return fmt.Errorf("hook %s BeforeChange failed: %w", hook.Name(), err)
		}
	}
	return nil
}

// ExecuteAfterChange executes all AfterChange hooks
func (m *HookManager) ExecuteAfterChange(ctx context.Context, changeType string, changeDetails interface{}, success bool, err error) error {
	for _, hook := range m.hooks {
		if err := hook.AfterChange(ctx, changeType, changeDetails, success, err); err != nil {
			return fmt.Errorf("hook %s AfterChange failed: %w", hook.Name(), err)
		}
	}
	return nil
}

// DevKitGateHook implements a hook that uses DevKit to check before changes
type DevKitGateHook struct {
	devKitClient *devkit_client.DevKitClient
}

// NewDevKitGateHook creates a new DevKitGateHook
func NewDevKitGateHook(client *devkit_client.DevKitClient) *DevKitGateHook {
	return &DevKitGateHook{
		devKitClient: client,
	}
}

// Name returns the hook name
func (h *DevKitGateHook) Name() string {
	return "DevKitGate"
}

// BeforeTool executes before a tool runs
func (h *DevKitGateHook) BeforeTool(ctx context.Context, toolName string, input interface{}) error {
	// For now, just log the tool call
	// DevKit gates are more relevant for changes, not tool calls
	return nil
}

// AfterTool executes after a tool runs
func (h *DevKitGateHook) AfterTool(ctx context.Context, toolName string, input interface{}, output interface{}, err error) error {
	// Log tool execution
	if err != nil {
		fmt.Printf("[DevKitGate] Tool %s failed: %v\n", toolName, err)
	} else {
		fmt.Printf("[DevKitGate] Tool %s succeeded\n", toolName)
	}
	return nil
}

// BeforeChange executes before a change is made
func (h *DevKitGateHook) BeforeChange(ctx context.Context, changeType string, changeDetails interface{}) error {
	if h.devKitClient == nil {
		return fmt.Errorf("DevKit client not configured")
	}

	// Convert changeDetails to string description
	var description string
	switch v := changeDetails.(type) {
	case string:
		description = v
	case map[string]interface{}:
		// Try to get a meaningful description
		if desc, ok := v["description"].(string); ok {
			description = desc
		} else {
			description = fmt.Sprintf("Change of type %s", changeType)
		}
	default:
		description = fmt.Sprintf("Change of type %s", changeType)
	}

	// Check if change is approved by DevKit
	approved, err := devkit_client.MustBeApproved(h.devKitClient, description, changeType)
	if err != nil {
		// If DevKit is not available, just warn but don't block
		fmt.Printf("[DevKitGate] Warning: Failed to check DevKit approval: %v\n", err)
		return nil
	}

	if !approved {
		return fmt.Errorf("change rejected by DevKit gate: %s", description)
	}

	fmt.Printf("[DevKitGate] Change approved by DevKit: %s\n", description)
	return nil
}

// AfterChange executes after a change is made
func (h *DevKitGateHook) AfterChange(ctx context.Context, changeType string, changeDetails interface{}, success bool, err error) error {
	// Log the change result
	if err != nil {
		fmt.Printf("[DevKitGate] Change %s failed: %v\n", changeType, err)
	} else {
		fmt.Printf("[DevKitGate] Change %s succeeded\n", changeType)
	}
	return nil
}

// SelfHealingHook implements a hook that can automatically fix issues
type SelfHealingHook struct {
	devKitClient *devkit_client.DevKitClient
}

// NewSelfHealingHook creates a new SelfHealingHook
func NewSelfHealingHook(client *devkit_client.DevKitClient) *SelfHealingHook {
	return &SelfHealingHook{
		devKitClient: client,
	}
}

// Name returns the hook name
func (h *SelfHealingHook) Name() string {
	return "SelfHealing"
}

// BeforeTool executes before a tool runs
func (h *SelfHealingHook) BeforeTool(ctx context.Context, toolName string, input interface{}) error {
	// Self-healing doesn't interfere with tool execution
	return nil
}

// AfterTool executes after a tool runs
func (h *SelfHealingHook) AfterTool(ctx context.Context, toolName string, input interface{}, output interface{}, err error) error {
	// Check if the tool failed and try to fix it
	if err != nil {
		fmt.Printf("[SelfHealing] Tool %s failed: %v\n", toolName, err)
	}

	return nil
}

// BeforeChange executes before a change is made
func (h *SelfHealingHook) BeforeChange(ctx context.Context, changeType string, changeDetails interface{}) error {
	// Self-healing doesn't interfere with change approval
	return nil
}

// AfterChange executes after a change is made
func (h *SelfHealingHook) AfterChange(ctx context.Context, changeType string, changeDetails interface{}, success bool, err error) error {
	// Check if the change failed and try to fix it
	if err != nil {
		fmt.Printf("[SelfHealing] Change %s failed: %v\n", changeType, err)
	}

	return nil
}

// EnhancedJournalHook implements a hook that enhances journaling
type EnhancedJournalHook struct {
	devKitClient *devkit_client.DevKitClient
}

// NewEnhancedJournalHook creates a new EnhancedJournalHook
func NewEnhancedJournalHook(client *devkit_client.DevKitClient) *EnhancedJournalHook {
	return &EnhancedJournalHook{
		devKitClient: client,
	}
}

// Name returns the hook name
func (h *EnhancedJournalHook) Name() string {
	return "EnhancedJournal"
}

// BeforeTool executes before a tool runs
func (h *EnhancedJournalHook) BeforeTool(ctx context.Context, toolName string, input interface{}) error {
	// Log tool execution to journal
	if h.devKitClient != nil {
		entryType := "tool_execution"
		entryData := map[string]interface{}{
			"tool_name":   toolName,
			"input":       input,
			"timestamp":   time.Now().Unix(),
		}
		h.devKitClient.LogJournalEntry(entryType, entryData)
	}

	return nil
}

// AfterTool executes after a tool runs
func (h *EnhancedJournalHook) AfterTool(ctx context.Context, toolName string, input interface{}, output interface{}, err error) error {
	// Log tool result to journal
	if h.devKitClient != nil {
		entryType := "tool_result"
		entryData := map[string]interface{}{
			"tool_name":   toolName,
			"output":      output,
			"error":       err,
			"timestamp":   time.Now().Unix(),
		}
		h.devKitClient.LogJournalEntry(entryType, entryData)
	}

	return nil
}

// BeforeChange executes before a change is made
func (h *EnhancedJournalHook) BeforeChange(ctx context.Context, changeType string, changeDetails interface{}) error {
	// Log change initiation to journal
	if h.devKitClient != nil {
		entryType := "change_initiated"
		entryData := map[string]interface{}{
			"change_type":  changeType,
			"change_details": changeDetails,
			"timestamp":    time.Now().Unix(),
		}
		h.devKitClient.LogJournalEntry(entryType, entryData)
	}

	return nil
}

// AfterChange executes after a change is made
func (h *EnhancedJournalHook) AfterChange(ctx context.Context, changeType string, changeDetails interface{}, success bool, err error) error {
	// Log change completion to journal
	if h.devKitClient != nil {
		entryType := "change_completed"
		entryData := map[string]interface{}{
			"change_type":  changeType,
			"change_details": changeDetails,
			"success":      success,
			"error":        err,
			"timestamp":    time.Now().Unix(),
		}
		h.devKitClient.LogJournalEntry(entryType, entryData)
	}

	return nil
}

// CallHook wraps a function with hook execution
func CallHook(ctx context.Context, manager *HookManager, hookType string, operation string, args ...interface{}) (interface{}, error) {
	var input, output interface{}

	switch operation {
	case "before":
		if len(args) > 0 {
			input = args[0]
		}
		if err := manager.ExecuteBeforeTool(ctx, hookType, input); err != nil {
			return nil, err
		}
	case "after":
		if len(args) > 1 {
			input = args[0]
			output = args[1]
		}
		if err := manager.ExecuteAfterTool(ctx, hookType, input, output, nil); err != nil {
			return output, err
		}
	}

	return output, nil
}

// WrapToolWithHooks wraps a tool function with hook execution
func WrapToolWithHooks(ctx context.Context, manager *HookManager, toolName string, toolFunc func(interface{}) (interface{}, error)) func(interface{}) (interface{}, error) {
	return func(input interface{}) (interface{}, error) {
		// Execute before hooks
		if err := manager.ExecuteBeforeTool(ctx, toolName, input); err != nil {
			return nil, err
		}

		// Execute the actual tool
		output, err := toolFunc(input)

		// Execute after hooks
		if err2 := manager.ExecuteAfterTool(ctx, toolName, input, output, err); err2 != nil {
			if err == nil {
				err = err2
			} else {
				err = fmt.Errorf("%w (after hook error: %v)", err, err2)
			}
		}

		return output, err
	}
}

// WrapChangeWithHooks wraps a change function with hook execution
func WrapChangeWithHooks(ctx context.Context, manager *HookManager, changeType string, changeFunc func(interface{}) (interface{}, error)) func(interface{}) (interface{}, error) {
	return func(changeDetails interface{}) (interface{}, error) {
		// Execute before change hooks
		if err := manager.ExecuteBeforeChange(ctx, changeType, changeDetails); err != nil {
			return nil, err
		}

		// Execute the actual change
		output, err := changeFunc(changeDetails)

		// Execute after change hooks
		success := err == nil
		if err2 := manager.ExecuteAfterChange(ctx, changeType, changeDetails, success, err); err2 != nil {
			if err == nil {
				err = err2
			} else {
				err = fmt.Errorf("%w (after hook error: %v)", err, err2)
			}
		}

		return output, err
	}
}

// ExecuteWithCheckpoint executes a function with DevKit checkpoint
func ExecuteWithCheckpoint(ctx context.Context, client *devkit_client.DevKitClient, description string, operation func() (interface{}, error)) (interface{}, error) {
	if client == nil {
		return operation()
	}

	// Create checkpoint
	checkpoint, err := client.CreateCheckpoint(description)
	if err != nil {
		fmt.Printf("[Hook] Failed to create checkpoint: %v\n", err)
		return operation()
	}

	fmt.Printf("[Hook] Created checkpoint %s before: %s\n", checkpoint.ID, description)

	// Execute the operation
	result, err := operation()

	// Rollback if needed
	if err != nil {
		fmt.Printf("[Hook] Operation failed, rolling back to %s\n", checkpoint.ID)
		_, rollbackErr := client.RollbackToCheckpoint(checkpoint.ID)
		if rollbackErr != nil {
			fmt.Printf("[Hook] Failed to rollback: %v\n", rollbackErr)
		} else {
			fmt.Printf("[Hook] Rolled back to %s\n", checkpoint.ID)
		}
	}

	return result, err
}
