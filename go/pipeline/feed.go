package pipeline

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

var (
	bridgeURL = "http://localhost:8001"
	httpClient = &http.Client{Timeout: 2 * time.Second}
)

type FeedSource struct {
	Name       string
	Endpoint   string
	Weight     float64
	Latency    time.Duration
	LastPrice  float64
	Timestamp  time.Time
	Connected  bool
	mu         sync.RWMutex
}

type UltraLowLatencyFeed struct {
	sources   []*FeedSource
	mu        sync.RWMutex
	stopChan  chan struct{}
	started   bool
}

func NewUltraLowLatencyFeed() *UltraLowLatencyFeed {
	return &UltraLowLatencyFeed{
		sources: []*FeedSource{
			{
				Name:      "MT5",
				Endpoint:  "ws://localhost:4438",
				Weight:    0.5,
				Latency:   5 * time.Millisecond,
			},
			{
				Name:      "AlphaVantage",
				Endpoint:  "https://www.alphavantage.co/query",
				Weight:    0.2,
				Latency:   120 * time.Millisecond,
			},
			{
				Name:      "Binance",
				Endpoint:  "wss://stream.binance.com:9443/ws",
				Weight:    0.3,
				Latency:   8 * time.Millisecond,
			},
		},
		stopChan: make(chan struct{}),
	}
}

func (f *UltraLowLatencyFeed) GetConsensus() float64 {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var weightedSum, totalWeight float64
	now := time.Now()

	for _, src := range f.sources {
		src.mu.RLock()
		if now.Sub(src.Timestamp) < 100*time.Millisecond && src.Connected {
			weightedSum += src.LastPrice * src.Weight
			totalWeight += src.Weight
		}
		src.mu.RUnlock()
	}

	if totalWeight == 0 {
		return 0
	}
	return weightedSum / totalWeight
}

func (f *UltraLowLatencyFeed) Start() {
	f.mu.Lock()
	if f.started {
		f.mu.Unlock()
		return
	}
	f.started = true
	f.mu.Unlock()

	for _, src := range f.sources {
		go f.runFeed(src)
	}
	go f.monitorHealth()
}

func (f *UltraLowLatencyFeed) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.started {
		return
	}
	close(f.stopChan)
	f.started = false
	f.stopChan = make(chan struct{})
}

func (f *UltraLowLatencyFeed) runFeed(src *FeedSource) {
	ticker := time.NewTicker(src.Latency / 2)
	defer ticker.Stop()

	for {
		select {
		case <-f.stopChan:
			return
		case <-ticker.C:
			price, err := f.fetchPrice(src)
			if err == nil {
				src.mu.Lock()
				src.LastPrice = price
				src.Timestamp = time.Now()
				src.Connected = true
				src.mu.Unlock()
			} else {
				src.mu.Lock()
				src.Connected = false
				src.mu.Unlock()
			}
		}
	}
}

func (f *UltraLowLatencyFeed) fetchPrice(src *FeedSource) (float64, error) {
	switch src.Name {
	case "MT5":
		return fetchFromBridge("/tick/EURUSD")
	default:
		return fetchFromBridge("/tick/EURUSD")
	}
}

func fetchFromBridge(path string) (float64, error) {
	resp, err := httpClient.Get(bridgeURL + path)
	if err != nil {
		return 0, fmt.Errorf("bridge unreachable: %w", err)
	}
	defer resp.Body.Close()

	var tick struct {
		Bid float64 `json:"bid"`
		Ask float64 `json:"ask"`
		Last float64 `json:"last"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tick); err != nil {
		return 0, fmt.Errorf("decode error: %w", err)
	}

	if tick.Last > 0 {
		return tick.Last, nil
	}
	if tick.Ask > 0 && tick.Bid > 0 {
		return (tick.Ask + tick.Bid) / 2, nil
	}
	if tick.Ask > 0 {
		return tick.Ask, nil
	}
	return tick.Bid, nil
}

func (f *UltraLowLatencyFeed) monitorHealth() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-f.stopChan:
			return
		case <-ticker.C:
			f.mu.RLock()
			connected := 0
			for _, src := range f.sources {
				src.mu.RLock()
				if src.Connected {
					connected++
				}
				src.mu.RUnlock()
			}
			f.mu.RUnlock()

			if connected == 0 {
				log.Printf("WARNING: No feed sources connected")
			}
		}
	}
}

func (f *UltraLowLatencyFeed) GetLatency() map[string]time.Duration {
	f.mu.RLock()
	defer f.mu.RUnlock()
	latencies := make(map[string]time.Duration)
	for _, src := range f.sources {
		latencies[src.Name] = src.Latency
	}
	return latencies
}

func (f *UltraLowLatencyFeed) GetSourceStatus() map[string]bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	status := make(map[string]bool)
	for _, src := range f.sources {
		src.mu.RLock()
		status[src.Name] = src.Connected
		src.mu.RUnlock()
	}
	return status
}

func (f *UltraLowLatencyFeed) UpdatePrice(sourceName string, price float64) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for _, src := range f.sources {
		if src.Name == sourceName {
			src.mu.Lock()
			src.LastPrice = price
			src.Timestamp = time.Now()
			src.Connected = true
			src.mu.Unlock()
			break
		}
	}
}

func (f *UltraLowLatencyFeed) GetPrices() map[string]float64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	prices := make(map[string]float64)
	for _, src := range f.sources {
		src.mu.RLock()
		prices[src.Name] = src.LastPrice
		src.mu.RUnlock()
	}
	return prices
}