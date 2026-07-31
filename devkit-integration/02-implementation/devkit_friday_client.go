// Copyright (c) 2026 Friday AI
// All rights reserved.
//
// This Friday DevKit Client is part of the Friday AI system.
// Friday is a complete AI agent system with 77 integrated tools.
// For more information, visit https://friday.ai
//
// Permission to use, modify, and distribute this software is
// granted provided that this copyright notice remains intact.

package devkit_client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// DevKitClient is a client that Friday can call to get DevKit services
type DevKitClient struct {
	BaseURL    string
	DevKitPort string
	AuthToken  string
	Timeout    time.Duration
}

// Init initializes the DevKit client
func (c *DevKitClient) Init(devKitPort, auth string) error {
	c.BaseURL = fmt.Sprintf("http://localhost:%s", devKitPort)
	c.DevKitPort = devKitPort
	c.AuthToken = auth
	c.Timeout = 30 * time.Second
	return nil
}

// CheckBeforeChange requests DevKit to check if a change is safe
func (c *DevKitClient) CheckBeforeChange(changeDescription string, changeType string) (*CheckResult, error) {
	url := fmt.Sprintf("%s/check-before-change", c.BaseURL)

	payload := map[string]interface{}{
		"description": changeDescription,
		"type":        changeType,
		"timestamp":   time.Now().Unix(),
		"agent":       "Friday",
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.AuthToken)
	req.Timeout = c.Timeout

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result CheckResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return &result, fmt.Errorf("DevKit returned status %d: %s", resp.StatusCode, result.Message)
	}

	return &result, nil
}

// RollbackToCheckpoint requests a rollback to a specific checkpoint
func (c *DevKitClient) RollbackToCheckpoint(checkpointID string) (*RollbackResult, error) {
	url := fmt.Sprintf("%s/rollback-to-checkpoint", c.BaseURL)

	payload := map[string]interface{}{
		"checkpointID": checkpointID,
		"timestamp":    time.Now().Unix(),
		"agent":        "Friday",
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.AuthToken)
	req.Timeout = c.Timeout

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result RollbackResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return &result, fmt.Errorf("DevKit returned status %d: %s", resp.StatusCode, result.Message)
	}

	return &result, nil
}

// CreateCheckpoint creates a new checkpoint
func (c *DevKitClient) CreateCheckpoint(description string) (*CheckpointResult, error) {
	url := fmt.Sprintf("%s/create-checkpoint", c.BaseURL)

	payload := map[string]interface{}{
		"description": description,
		"timestamp":   time.Now().Unix(),
		"agent":       "Friday",
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.AuthToken)
	req.Timeout = c.Timeout

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result CheckpointResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return &result, fmt.Errorf("DevKit returned status %d: %s", resp.StatusCode, result.Message)
	}

	return &result, nil
}

// GetJournal returns journal entries
func (c *DevKitClient) GetJournal(limit int) ([]JournalEntry, error) {
	url := fmt.Sprintf("%s/journal?limit=%d", c.BaseURL, limit)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", c.AuthToken)
	req.Timeout = c.Timeout

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var entries []JournalEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return entries, fmt.Errorf("DevKit returned status %d", resp.StatusCode)
	}

	return entries, nil
}

// GetAvailableSkills returns available DevKit skills
func (c *DevKitClient) GetAvailableSkills() ([]DevKitSkill, error) {
	url := fmt.Sprintf("%s/skills", c.BaseURL)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", c.AuthToken)
	req.Timeout = c.Timeout

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var skills []DevKitSkill
	if err := json.Unmarshal(body, &skills); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return skills, fmt.Errorf("DevKit returned status %d", resp.StatusCode)
	}

	return skills, nil
}

// HealthCheck performs a health check with DevKit
func (c *DevKitClient) HealthCheck() (*HealthCheckResult, error) {
	url := fmt.Sprintf("%s/health", c.BaseURL)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", c.AuthToken)
	req.Timeout = c.Timeout

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result HealthCheckResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return &result, fmt.Errorf("DevKit returned status %d: %s", resp.StatusCode, result.Message)
	}

	return &result, nil
}

// LogJournalEntry creates a journal entry
func (c *DevKitClient) LogJournalEntry(entryType string, entryData map[string]interface{}) (*JournalEntry, error) {
	url := fmt.Sprintf("%s/journal", c.BaseURL)

	payload := map[string]interface{}{
		"type":        entryType,
		"data":        entryData,
		"agent":       "Friday",
		"timestamp":   time.Now().Unix(),
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.AuthToken)
	req.Timeout = c.Timeout

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var entry JournalEntry
	if err := json.Unmarshal(body, &entry); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return &entry, fmt.Errorf("DevKit returned status %d: %s", resp.StatusCode, entry.Message)
	}

	return &entry, nil
}

// DevKitClient functions for Friday integration

// NewDevKitClient creates a new DevKit client
func NewDevKitClient(devKitPort, auth string) *DevKitClient {
	return &DevKitClient{
		DevKitPort: devKitPort,
		AuthToken:  auth,
	}
}

// GetDevKitPortFromEnv gets DevKit port from environment
func GetDevKitPortFromEnv() string {
	port := os.Getenv("DEVKIT_PORT")
	if port == "" {
		port = "8080"
	}
	return port
}

// GetDevKitAuthTokenFromEnv gets auth token from environment
func GetDevKitAuthTokenFromEnv() string {
	token := os.Getenv("DEVKIT_AUTH_TOKEN")
	if token == "" {
		token = "friday-auth-token"
	}
	return token
}

// IntegrationResult represents the result of an integration operation
type IntegrationResult struct {
	Success  bool   `json:"success"`
	Message  string `json:"message"`
	ChangeID string `json:"change_id,omitempty"`
}

// ExecuteIntegration wraps a change with DevKit integration
func ExecuteIntegration(client *DevKitClient, description, changeType string, operation func() (interface{}, error)) (*IntegrationResult, error) {
	result := &IntegrationResult{
		Success: false,
	}

	// Create checkpoint before operation
	checkpoint, err := client.CreateCheckpoint(description)
	if err != nil {
		return result, fmt.Errorf("failed to create checkpoint: %w", err)
	}

	result.ChangeID = checkpoint.ID
	result.Message = fmt.Sprintf("Created checkpoint %s before operation", checkpoint.ID)

	// Execute the actual operation
	data, err := operation()
	if err != nil {
		result.Message = fmt.Sprintf("Operation failed: %w", err)

		// Try to rollback
		rollbackResult, rollbackErr := client.RollbackToCheckpoint(checkpoint.ID)
		if rollbackErr != nil {
			result.Message = fmt.Sprintf("%s\nFailed to rollback: %w", result.Message, rollbackErr)
		} else {
			result.Message = fmt.Sprintf("%s\nRollback to %s: %s", result.Message, rollbackResult.CheckpointID, rollbackResult.Message)
		}

		return result, err
	}

	result.Success = true
	result.Message = "Operation completed successfully"

	return result, nil
}

// MustBeApproved checks if a change must be approved before proceeding
func MustBeApproved(client *DevKitClient, description, changeType string) (bool, error) {
	checkResult, err := client.CheckBeforeChange(description, changeType)
	if err != nil {
		return false, fmt.Errorf("failed to check if change is approved: %w", err)
	}

	if !checkResult.IsApproved {
		return false, fmt.Errorf("change not approved by DevKit: %s", checkResult.Message)
	}

	return true, nil
}

// CheckBeforeApprove is a convenience function for checking approval
func CheckBeforeApprove(client *DevKitClient, description, changeType string) (string, error) {
	checkResult, err := client.CheckBeforeChange(description, changeType)
	if err != nil {
		return "", fmt.Errorf("failed to check change: %w", err)
	}

	if checkResult.IsApproved {
		return checkResult.ChangeID, nil
	}

	return "", fmt.Errorf("change requires approval: %s", checkResult.Message)
}

// DevKitClient API response types

type CheckResult struct {
	Success     bool   `json:"success"`
	IsApproved  bool   `json:"is_approved"`
	ChangeID    string `json:"change_id,omitempty"`
	Message     string `json:"message"`
	Errors      []string `json:"errors,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
}

type RollbackResult struct {
	Success     bool   `json:"success"`
	RolledBack  bool   `json:"rolled_back"`
	Message     string `json:"message"`
	CheckpointID string `json:"checkpoint_id,omitempty"`
}

type CheckpointResult struct {
	Success    bool   `json:"success"`
	Checkpoint *CheckpointInfo `json:"checkpoint,omitempty"`
	Message    string `json:"message"`
}

type CheckpointInfo struct {
	ID          string    `json:"id"`
	Description string    `json:"description"`
	Timestamp   int64     `json:"timestamp"`
	CreatedBy   string    `json:"created_by"`
}

type JournalEntry struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Data        map[string]interface{} `json:"data"`
	Timestamp   int64                  `json:"timestamp"`
	CreatedBy   string                 `json:"created_by"`
	Message     string                 `json:"message,omitempty"`
}

type DevKitSkill struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

type HealthCheckResult struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	Version   string `json:"version,omitempty"`
	Uptime    int64  `json:"uptime,omitempty"`
}
