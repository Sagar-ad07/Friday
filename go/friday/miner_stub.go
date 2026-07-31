package friday

import (
	"sync"
)

// MinerManager provides miner status - stub for now
type MinerManager struct {
	mu           sync.RWMutex
	Running      bool
	PID          int
	Hashrate1m   float64
	Hashrate15m  float64
	HashratePeak float64
	SharesGood   int64
	SharesTotal  int64
	Accepted     int64
	Rejected     int64
	UptimeSeconds int64
	PoolURL      string
	PoolConnected bool
	PoolUptimeSeconds int64
	WorkerID     string
	WalletConfigured bool
}

var minerManagerInstance *MinerManager
var minerOnce sync.Once

func GetMinerManager() *MinerManager {
	minerOnce.Do(func() {
		minerManagerInstance = &MinerManager{
			WalletConfigured: false,
		}
	})
	return minerManagerInstance
}

func (m *MinerManager) GetSummary() *MinerManager {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return &MinerManager{
		Running:          m.Running,
		PID:              m.PID,
		Hashrate1m:       m.Hashrate1m,
		Hashrate15m:      m.Hashrate15m,
		HashratePeak:     m.HashratePeak,
		SharesGood:       m.SharesGood,
		SharesTotal:      m.SharesTotal,
		Accepted:         m.Accepted,
		Rejected:         m.Rejected,
		UptimeSeconds:    m.UptimeSeconds,
		PoolURL:          m.PoolURL,
		PoolConnected:    m.PoolConnected,
		PoolUptimeSeconds: m.PoolUptimeSeconds,
		WorkerID:         m.WorkerID,
		WalletConfigured: m.WalletConfigured,
	}
}

func (m *MinerManager) Restart() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Running = true
	m.PID = 12345
	m.Hashrate1m = 1200
	m.PoolURL = ""
	m.PoolConnected = true
	m.WorkerID = "FridayBot"
	return nil
}