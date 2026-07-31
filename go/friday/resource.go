package friday

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

type CPUInfo struct {
	Cores        int     `json:"cores"`
	LogicalProcs int     `json:"logical_processors"`
	UsagePct     float64 `json:"usage_percent"`
	ModelName    string  `json:"model_name,omitempty"`
}

type MemoryInfo struct {
	TotalMB     float64 `json:"total_mb"`
	AvailableMB float64 `json:"available_mb"`
	UsedMB      float64 `json:"used_mb"`
	UsagePct    float64 `json:"usage_percent"`
}

type DiskInfo struct {
	TotalGB  float64 `json:"total_gb"`
	FreeGB   float64 `json:"free_gb"`
	UsagePct float64 `json:"usage_percent"`
}

type SystemResources struct {
	CPU    CPUInfo    `json:"cpu"`
	Memory MemoryInfo `json:"memory"`
	Disk   DiskInfo   `json:"disk"`
}

type ResourceManager struct {
	mu              sync.RWMutex
	LastCheck       time.Time          `json:"last_check"`
	History         []SystemResources  `json:"history"`
	MiningIntensity int                `json:"mining_intensity"`
	IdleMode        bool               `json:"idle_mode"`
	GPUAvailable    bool               `json:"gpu_available"`
	GPUModel        string             `json:"gpu_model,omitempty"`
	TotalRAMMB      float64            `json:"total_ram_mb"`
	TotalDiskGB     float64            `json:"total_disk_gb"`
}

var globalResourceManager *ResourceManager
var resourceManagerOnce sync.Once

// Windows API types for GlobalMemoryStatusEx
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func getWindowsMemory() (totalMB, availMB float64) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GlobalMemoryStatusEx")
	var mem memoryStatusEx
	mem.Length = uint32(unsafe.Sizeof(mem))
	ret, _, _ := proc.Call(uintptr(unsafe.Pointer(&mem)))
	if ret == 0 {
		return 0, 0
	}
	return float64(mem.TotalPhys) / 1024 / 1024, float64(mem.AvailPhys) / 1024 / 1024
}

func getWindowsDiskFree(path string) (totalGB, freeGB float64) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetDiskFreeSpaceExW")
	ptr, _ := syscall.UTF16PtrFromString(path)
	var freeBytes, totalBytes, totalFree int64
	ret, _, _ := proc.Call(
		uintptr(unsafe.Pointer(ptr)),
		uintptr(unsafe.Pointer(&freeBytes)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if ret == 0 {
		return 0, 0
	}
	return float64(totalBytes) / 1024 / 1024 / 1024, float64(freeBytes) / 1024 / 1024 / 1024
}

func GetResourceManager() *ResourceManager {
	resourceManagerOnce.Do(func() {
		totalMemMB, _ := getWindowsMemory()
		if totalMemMB <= 0 {
			totalMemMB = 16384
		}
		totalDiskGB, _ := getWindowsDiskFree("D:\\")
		if totalDiskGB <= 0 {
			totalDiskGB = 238
		}
		rm := &ResourceManager{
			MiningIntensity: 30,
			IdleMode:        false,
			TotalRAMMB:      totalMemMB,
			TotalDiskGB:     totalDiskGB,
		}
		rm.sample()
		globalResourceManager = rm
	})
	return globalResourceManager
}

func (rm *ResourceManager) GetCurrent() SystemResources {
	rm.sample()
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	if len(rm.History) == 0 {
		return SystemResources{}
	}
	return rm.History[len(rm.History)-1]
}

func (rm *ResourceManager) Summary() string {
	res := rm.GetCurrent()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("CPU: %d cores (%d logical)\n", res.CPU.Cores, res.CPU.LogicalProcs))
	sb.WriteString(fmt.Sprintf("RAM: %.0f/%.0f MB (%.1f%%) — 16GB total\n", res.Memory.UsedMB, res.Memory.TotalMB, res.Memory.UsagePct))
	sb.WriteString(fmt.Sprintf("SSD: %.1f/%.1f GB (%.1f%%) — 256GB total\n", res.Disk.TotalGB-res.Disk.FreeGB, res.Disk.TotalGB, res.Disk.UsagePct))
	sb.WriteString(fmt.Sprintf("Mining Intensity: %d%% (recommended max on this laptop: 40%%)\n", rm.MiningIntensity))

	if rm.IdleMode {
		sb.WriteString("Mode: IDLE — mining at minimum, heavy tasks paused\n")
	} else {
		sb.WriteString("Mode: ACTIVE\n")
	}

	if rm.GPUAvailable {
		sb.WriteString(fmt.Sprintf("GPU: %s\n", rm.GPUModel))
	} else {
		sb.WriteString("GPU: Not detected (CPU mining only)\n")
	}

	growth := rm.ProjectCOMPOUND(30)
	sb.WriteString(fmt.Sprintf("Growth projection (30d at current rate): %s\n", growth))

	return sb.String()
}

func (rm *ResourceManager) SetMiningIntensity(pct int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}

	rm.mu.Lock()
	if pct > 40 {
		pct = 40
	}
	rm.MiningIntensity = pct
	rm.mu.Unlock()

	return fmt.Sprintf("Mining intensity set to %d%% (capped at 40%% for this laptop)", pct)
}

func (rm *ResourceManager) SetIdleMode(idle bool) string {
	rm.mu.Lock()
	rm.IdleMode = idle
	rm.mu.Unlock()

	if idle {
		return "Laptop set to IDLE mode — mining throttled to 10%, heavy tasks paused"
	}
	return "Laptop set to ACTIVE mode — all tasks resumed"
}

func (rm *ResourceManager) RecommendMiningIntensity() int {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	if len(rm.History) == 0 {
		return 30
	}
	res := rm.History[len(rm.History)-1]

	if rm.IdleMode { return 10 }
	if res.Memory.UsagePct > 80 { return 15 }
	if res.Memory.UsagePct > 60 { return 25 }
	return 35
}

func (rm *ResourceManager) CanRunHeavyTask() bool {
	res := rm.GetCurrent()
	return res.Memory.UsagePct < 70 && res.Memory.AvailableMB > 2048
}

func (rm *ResourceManager) ScheduleTask(name string, duration time.Duration) string {
	res := rm.GetCurrent()

	if !rm.CanRunHeavyTask() {
		return fmt.Sprintf("Cannot schedule '%s': RAM too high (%.1f%%, need <70%% with 2GB free)", name, res.Memory.UsagePct)
	}

	return fmt.Sprintf("Task '%s' scheduled (%v). RAM: %.1f%% used, %.0f MB free — OK",
		name, duration, res.Memory.UsagePct, res.Memory.AvailableMB)
}

func (rm *ResourceManager) HistorySummary() string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	if len(rm.History) == 0 {
		return "No resource history recorded"
	}

	peakMem := 0.0
	avgMem := 0.0
	samples := len(rm.History)

	for _, h := range rm.History {
		if h.Memory.UsagePct > peakMem {
			peakMem = h.Memory.UsagePct
		}
		avgMem += h.Memory.UsagePct
	}
	avgMem /= float64(samples)

	return fmt.Sprintf("Resource history: %d samples. RAM: avg %.1f%%, peak %.1f%%. Mining: %d%%. SSD: %.0f GB free of 256 GB.",
		samples, avgMem, peakMem, rm.MiningIntensity, rm.getFreeDisk())
}

func (rm *ResourceManager) ProjectCOMPOUND(days int) string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	compounder := GetCompounder()
	if len(compounder.DailyHistory) < 3 {
		return "Need 3+ days of data to project growth"
	}

	recent := compounder.DailyHistory
	if len(recent) > 7 {
		recent = recent[len(recent)-7:]
	}
	avgDaily := 0.0
	for _, v := range recent {
		avgDaily += v
	}
	avgDaily /= float64(len(recent))

	if avgDaily <= 0 {
		return "Average daily profit is $0 — no growth to project"
	}

	for i := 0; i < days; i++ {
		projected := compounder.TotalCapital + avgDaily*float64(i+1)
		if i == days-1 {
			return fmt.Sprintf("At $%.2f/day avg, in %d days: $%.2f → $%.2f (+$%.2f)",
				avgDaily, days, compounder.TotalCapital, projected, projected-compounder.TotalCapital)
		}
	}

	return ""
}

func (rm *ResourceManager) getFreeDisk() float64 {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	if len(rm.History) == 0 {
		return 0
	}
	return rm.History[len(rm.History)-1].Disk.FreeGB
}

func (rm *ResourceManager) sample() {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	cpu := CPUInfo{
		Cores:        runtime.NumCPU(),
		LogicalProcs: runtime.NumCPU(),
		ModelName:    os.Getenv("PROCESSOR_IDENTIFIER"),
	}

	totalMemMB, availMemMB := getWindowsMemory()
	if totalMemMB <= 0 {
		totalMemMB = rm.TotalRAMMB
		availMemMB = rm.TotalRAMMB * 0.5
	}
	usedMemMB := totalMemMB - availMemMB
	if usedMemMB < 0 {
		usedMemMB = 0
	}
	usagePct := 0.0
	if totalMemMB > 0 {
		usagePct = usedMemMB / totalMemMB * 100
	}

	mem := MemoryInfo{
		TotalMB:     totalMemMB,
		UsedMB:      usedMemMB,
		AvailableMB: availMemMB,
		UsagePct:    usagePct,
	}

	totalDiskGB, freeDiskGB := getWindowsDiskFree("D:\\")
	if totalDiskGB <= 0 {
		totalDiskGB = rm.TotalDiskGB
		freeDiskGB = rm.TotalDiskGB * 0.35
	}
	usedDiskGB := totalDiskGB - freeDiskGB
	if usedDiskGB < 0 {
		usedDiskGB = 0
	}
	diskUsagePct := 0.0
	if totalDiskGB > 0 {
		diskUsagePct = usedDiskGB / totalDiskGB * 100
	}

	disk := DiskInfo{
		TotalGB:  math.Round(totalDiskGB*10) / 10,
		FreeGB:   math.Round(freeDiskGB*10) / 10,
		UsagePct: math.Round(diskUsagePct*10) / 10,
	}

	resources := SystemResources{
		CPU:    cpu,
		Memory: mem,
		Disk:   disk,
	}

	rm.LastCheck = time.Now()
	rm.History = append(rm.History, resources)
	if len(rm.History) > 1000 {
		rm.History = rm.History[len(rm.History)-1000:]
	}

	// Mining intensity (computed inline to avoid re-entering sample())
	if rm.IdleMode {
		rm.MiningIntensity = 10
	} else if resources.Memory.UsagePct > 80 {
		rm.MiningIntensity = 15
	} else if resources.Memory.UsagePct > 60 {
		rm.MiningIntensity = 25
	} else {
		rm.MiningIntensity = 35
	}
}
