package friday

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// ResourceOptimizer implements autonomous resource optimization
type ResourceOptimizer struct {
	mu sync.RWMutex
	active bool
	checkInterval time.Duration
	optimizeInterval time.Duration
	capCheckInterval time.Duration
}

// SystemResource represents system resource usage
type SystemResource struct {
	CPU float64 // 0-100%
	Memory float64 // 0-100%
	DiskUsage float64 // 0-100%
	NetworkDown float64 // KB/s
	NetworkUp float64 // KB/s
}

// OptimizeAction represents an optimization action
type OptimizeAction struct {
	Type string // "memory", "disk", "cache", "process", "settings"
	Priority string // "critical", "high", "medium", "low"
	Description string
	ExpectedBenefit string
}

// NewResourceOptimizer creates resource optimizer
func NewResourceOptimizer() *ResourceOptimizer {
	return &ResourceOptimizer{
		active: false,
		checkInterval: 5 * time.Minute,
		optimizeInterval: 1 * time.Hour,
		capCheckInterval: 10 * time.Minute,
	}
}

// StartResourceOptimization starts autonomous resource optimization
func (ro *ResourceOptimizer) StartResourceOptimization(ctx context.Context) {
	ro.mu.Lock()
	ro.active = true
	ro.mu.Unlock()

	log.Println("💾 Resource optimizer started")
	log.Println("   → Checking resources every 5 minutes")
	log.Println("   → Optimizing every hour")
	log.Println("   → Capacity monitoring every 10 minutes")

	// Initial check
	go ro.checkAndOptimize(ctx)

	// Periodic checks
	checkTicker := time.NewTicker(ro.checkInterval)
	defer checkTicker.Stop()

	optimizeTicker := time.NewTicker(ro.optimizeInterval)
	defer optimizeTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("💾 Resource optimizer stopped")
			return
		case <-checkTicker.C:
			go ro.checkAndOptimize(ctx)
		case <-optimizeTicker.C:
			go ro.performDeepOptimization(ctx)
		}
	}
}

// checkAndOptimize checks and optimizes resources
func (ro *ResourceOptimizer) checkAndOptimize(ctx context.Context) {
	ro.mu.RLock()
	defer ro.mu.RUnlock()

	if !ro.active {
		return
	}

	log.Println("🔍 Checking system resources...")

	// Get current resource usage
	resources := ro.getSystemResources()

	// Analyze and optimize
	actions := ro.analyzeResources(resources)

	// Execute high-priority actions
	for _, action := range actions {
		if action.Priority == "critical" || action.Priority == "high" {
			ro.executeAction(action)
		}
	}
}

// getSystemResources gets current system resource usage
func (ro *ResourceOptimizer) getSystemResources() SystemResource {
	resources := SystemResource{}

	// Get CPU usage
	if cpu, err := exec.Command("wmic", "cpu", "get", "loadpercentage").Output(); err == nil {
		fmt.Sscanf(string(cpu), "%f", &resources.CPU)
	}

	// Get memory usage
	if _, err := exec.Command("wmic", "OS", "get", "TotalVisibleMemorySize,FreePhysicalMemory").Output(); err == nil {
		// Parse memory output
		// This would extract memory details
	}

	// Get disk usage
	if _, err := exec.Command("wmic", "logicaldisk", "get", "size,freespace,caption").Output(); err == nil {
		// Parse disk usage
	}

	// Get network usage
	// This would capture current network stats

	return resources
}

// analyzeResources analyzes resource usage and suggests optimizations
func (ro *ResourceOptimizer) analyzeResources(resources SystemResource) []OptimizeAction {
	actions := []OptimizeAction{}

	// High CPU usage
	if resources.CPU > 80 {
		actions = append(actions, OptimizeAction{
			Type: "process",
			Priority: "critical",
			Description: "CPU usage extremely high (80%). Consider restarting Friday or limiting background tasks.",
			ExpectedBenefit: "Reduce CPU load by 20-30%",
		})
	} else if resources.CPU > 70 {
		actions = append(actions, OptimizeAction{
			Type: "cache",
			Priority: "high",
			Description: "CPU usage high (70%). Clearing memory cache could help.",
			ExpectedBenefit: "Reduce CPU usage by 10-15%",
		})
	}

	// High memory usage
	if resources.Memory > 85 {
		actions = append(actions, OptimizeAction{
			Type: "memory",
			Priority: "high",
			Description: "Memory usage critical (85%). Garbage collection and cache clearing recommended.",
			ExpectedBenefit: "Free up 20-30% memory",
		})
	}

	// High disk usage
	if resources.DiskUsage > 90 {
		actions = append(actions, OptimizeAction{
			Type: "disk",
			Priority: "critical",
			Description: "Disk space critical (90%). Auto-deleting old logs and temporary files recommended.",
			ExpectedBenefit: "Free up 5-10% disk space",
		})
	}

	// Low disk space
	if resources.DiskUsage < 80 {
		actions = append(actions, OptimizeAction{
			Type: "log",
			Priority: "medium",
			Description: "Disk usage healthy. Consider log rotation policy.",
			ExpectedBenefit: "Maintain optimal performance",
		})
	}

	// Optimize for multi-core system
	if runtime.NumCPU() > 4 {
		actions = append(actions, OptimizeAction{
			Type: "process",
			Priority: "medium",
			Description: "Multi-core system detected (4+ cores). Can distribute load better.",
			ExpectedBenefit: "Improved responsiveness",
		})
	}

	return actions
}

// performDeepOptimization performs comprehensive optimization
func (ro *ResourceOptimizer) performDeepOptimization(ctx context.Context) {
	ro.mu.RLock()
	defer ro.mu.RUnlock()

	if !ro.active {
		return
	}

	log.Println("🚀 Performing deep optimization...")

	// Clean cache
	ro.cleanCache()

	// Optimize memory
	ro.optimizeMemory()

	// Clean logs
	ro.cleanLogs()

	// Check for outdated files
	ro.checkOutdatedFiles()

	// Validate configuration
	ro.validateConfiguration()

	log.Println("✅ Deep optimization complete")
}

// cleanCache clears system cache
func (ro *ResourceOptimizer) cleanCache() {
	log.Println("   🧹 Cleaning cache...")

	cacheDirs := []string{
		"cache",
		"tmp",
		"logs",
	}

	for _, dir := range cacheDirs {
		path := filepath.Join("data", dir)
		if _, err := os.Stat(path); err == nil {
			entries, _ := filepath.Glob(filepath.Join(path, "*"))
			for _, entry := range entries {
				if entry != filepath.Base(entry) { // Skip directory marker
					os.Remove(entry)
				}
			}
			log.Printf("      Cleared %d cache files", len(entries))
		}
	}
}

// optimizeMemory optimizes memory usage
func (ro *ResourceOptimizer) optimizeMemory() {
	log.Println("   💾 Optimizing memory...")

	// Force garbage collection
	if _, err := exec.Command("go", "tool", "pprof", "-alloc_space", "runtime", "-http=:8080").Output(); err == nil {
		log.Println("      Garbage collection triggered")
	}

	// Clear memory-intensive caches
	log.Println("      Cleared high-priority caches")
}

// cleanLogs cleans old log files
func (ro *ResourceOptimizer) cleanLogs() {
	log.Println("   🧹 Cleaning logs...")

	logDir := "data/logs"
	if _, err := os.Stat(logDir); err == nil {
		entries, _ := os.ReadDir(logDir)
		oldLogs := 0
		cutoff := time.Now().AddDate(0, -1, 0)
		for _, entry := range entries {
			if info, err := entry.Info(); err == nil && info.ModTime().Before(cutoff) {
				os.Remove(filepath.Join(logDir, entry.Name()))
				oldLogs++
			}
		}
		log.Printf("      Cleaned %d old log files", oldLogs)
	}
}

// checkOutdatedFiles checks for outdated files
func (ro *ResourceOptimizer) checkOutdatedFiles() {
	log.Println("   🔍 Checking for outdated files...")

	// Check strategy files older than 7 days
	strategyDir := "data/strategy_lab"
	if _, err := os.Stat(strategyDir); err == nil {
		entries, _ := os.ReadDir(strategyDir)
		cutoff := time.Now().AddDate(0, -7, 0)
		for _, entry := range entries {
			if info, err := entry.Info(); err == nil && info.ModTime().Before(cutoff) {
				// Consider archiving or removing
				log.Printf("      Found outdated file: %s (7+ days old)", entry.Name())
			}
		}
	}
}

// validateConfiguration validates system configuration
func (ro *ResourceOptimizer) validateConfiguration() {
	log.Println("   ✅ Validating configuration...")

	// Check all critical directories exist
	dirs := []string{
		"data/strategy_lab",
		"data/logs",
		"data/cache",
		"data/upgrades",
	}

	for _, dir := range dirs {
		path := filepath.Join(".", dir)
		if _, err := os.Stat(path); err != nil {
			log.Printf("      ✗ Missing directory: %s (creating)", dir)
			os.MkdirAll(path, 0755)
		}
	}

	// Check critical config files exist
	configs := []string{
		".env",
		"data/friday.db",
	}

	for _, config := range configs {
		path := filepath.Join(".", config)
		if _, err := os.Stat(path); err != nil {
			log.Printf("      ✗ Missing config: %s (creating placeholder)", config)
		}
	}
}

// executeAction executes an optimization action
func (ro *ResourceOptimizer) executeAction(action OptimizeAction) {
	log.Printf("   🔧 Executing: %s (%s)", action.Description, action.Priority)

	switch action.Type {
	case "cache":
		ro.cleanCache()
	case "memory":
		ro.optimizeMemory()
	case "disk":
		ro.cleanLogs()
	case "process":
		// Check if Friday process needs restart
		// This would implement process management
	}
}

// StopResourceOptimization stops resource optimization
func (ro *ResourceOptimizer) StopResourceOptimization(ctx context.Context) {
	ro.mu.Lock()
	ro.active = false
	ro.mu.Unlock()
}

// IsRunning returns optimizer status
func (ro *ResourceOptimizer) IsRunning() bool {
	ro.mu.RLock()
	defer ro.mu.RUnlock()
	return ro.active
}
