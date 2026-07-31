package friday

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type IncomeStream struct {
	Name           string    `json:"name"`
	Balance        float64   `json:"balance"`
	TotalProfit    float64   `json:"total_profit"`
	ProfitToday    float64   `json:"profit_today"`
	Profit7d       float64   `json:"profit_7d"`
	Profit30d      float64   `json:"profit_30d"`
	DailyProfits   []float64 `json:"daily_profits,omitempty"`
	LastUpdated    time.Time `json:"last_updated"`
}

type Compounder struct {
	mu           sync.RWMutex
	TotalCapital float64        `json:"total_capital"`
	InitialSeed  float64        `json:"initial_seed"`
	Streams      []IncomeStream `json:"streams"`
	AutoReinvest bool           `json:"auto_reinvest"`
	GrowthRate7d float64        `json:"growth_rate_7d"`
	GrowthRate30d float64       `json:"growth_rate_30d"`
	DailyHistory []float64      `json:"daily_history"`
	LastDailyLog time.Time      `json:"last_daily_log"`
	dirty        bool
}

var globalCompounder *Compounder
var compounderOnce sync.Once

func compounderPath() string {
	return filepath.Join(ProjectRoot, "data", "compounder.json")
}

func GetCompounder() *Compounder {
	compounderOnce.Do(func() {
		globalCompounder = loadCompounder()
		globalCompounder.save() // persist defaults so file exists
	})
	return globalCompounder
}

func loadCompounder() *Compounder {
	path := compounderPath()
	data, err := os.ReadFile(path)
	if err == nil {
		var c Compounder
		if json.Unmarshal(data, &c) == nil {
			log.Printf("Compounder loaded: %d streams, cap=$%.0f", len(c.Streams), c.TotalCapital)
			return &c
		}
	}
	return &Compounder{
		TotalCapital: 0,
		InitialSeed:  0,
		Streams: []IncomeStream{
			{Name: "Exness MT5 Trading", Balance: 5000},
			{Name: "Blue Guardian $50K", Balance: 50000},
		},
		AutoReinvest: true,
		DailyHistory: []float64{},
	}
}

func (c *Compounder) save() {
	p := compounderPath()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		log.Printf("Compounder mkdir error: %v", err)
		return
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		log.Printf("Compounder marshal error: %v", err)
		return
	}
	if err := os.WriteFile(p, data, 0644); err != nil {
		log.Printf("Compounder write error: %v", err)
		return
	}
	log.Printf("Compounder saved to %s (%d bytes)", p, len(data))
}

func (c *Compounder) UpdateStream(name string, profitToday, totalProfit float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i, s := range c.Streams {
		if s.Name == name {
			c.Streams[i].ProfitToday = profitToday
			c.Streams[i].TotalProfit = totalProfit
			c.Streams[i].LastUpdated = time.Now()
			break
		}
	}
	c.recalcTotal()
	c.save()
}

func (c *Compounder) Rebalance(targetAllocations map[string]float64) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	total := 0.0
	for _, s := range c.Streams {
		total += s.Balance + s.TotalProfit
	}
	if total <= 0 {
		return "No capital to rebalance"
	}
	for i, s := range c.Streams {
		targetPct, ok := targetAllocations[s.Name]
		if !ok { continue }
		currentVal := s.Balance + s.TotalProfit
		targetVal := total * targetPct / 100.0
		if targetVal > currentVal {
			c.Streams[i].Balance += targetVal - currentVal
		} else {
			excess := currentVal - targetVal
			profitToPull := math.Min(excess, c.Streams[i].TotalProfit)
			c.Streams[i].TotalProfit -= profitToPull
		}
	}
	c.recalcTotal()
	c.save()
	return fmt.Sprintf("Rebalanced across %d streams", len(c.Streams))
}

func (c *Compounder) DailyLog() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if !c.LastDailyLog.IsZero() && now.Day() == c.LastDailyLog.Day() && now.Month() == c.LastDailyLog.Month() && now.Year() == c.LastDailyLog.Year() {
		return "Already logged today"
	}

	todayProfit := 0.0
	for i, s := range c.Streams {
		if s.ProfitToday != 0 {
			c.Streams[i].DailyProfits = append(c.Streams[i].DailyProfits, s.ProfitToday)
			if len(c.Streams[i].DailyProfits) > 365 {
				c.Streams[i].DailyProfits = c.Streams[i].DailyProfits[len(c.Streams[i].DailyProfits)-365:]
			}
		}
		todayProfit += s.ProfitToday
	}
	c.DailyHistory = append(c.DailyHistory, todayProfit)
	if len(c.DailyHistory) > 365 {
		c.DailyHistory = c.DailyHistory[len(c.DailyHistory)-365:]
	}
	c.LastDailyLog = now
	for i := range c.Streams {
		c.Streams[i].ProfitToday = 0
	}
	// Recalculate 7d/30d profits from daily history before saving
	c.recalcProfits()
	c.recalcTotal()
	c.save()
	return fmt.Sprintf("Daily log: +$%.2f across all streams. Total capital: $%.2f", todayProfit, c.TotalCapital)
}

func (c *Compounder) recalcProfits() {
	for i := range c.Streams {
		dps := c.Streams[i].DailyProfits
		c.Streams[i].Profit7d = 0
		c.Streams[i].Profit30d = 0
		if len(dps) == 0 {
			continue
		}
		// Sum last 7 entries
		start7 := len(dps) - 7
		if start7 < 0 {
			start7 = 0
		}
		for j := start7; j < len(dps); j++ {
			c.Streams[i].Profit7d += dps[j]
		}
		// Sum last 30 entries
		start30 := len(dps) - 30
		if start30 < 0 {
			start30 = 0
		}
		for j := start30; j < len(dps); j++ {
			c.Streams[i].Profit30d += dps[j]
		}
	}
}

func (c *Compounder) Summary() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	s := fmt.Sprintf("Total Capital: $%.2f (seed: $%.2f)\n", c.TotalCapital, c.InitialSeed)
	s += fmt.Sprintf("Auto-Reinvest: %v\n", c.AutoReinvest)
	s += "Streams:\n"
	for _, stream := range c.Streams {
		s += fmt.Sprintf("  - %s: Balance=$%.2f, Total P&L=$%.2f, Today=$%.2f, 7d=$%.2f, 30d=$%.2f\n",
			stream.Name, stream.Balance, stream.TotalProfit,
			stream.ProfitToday, stream.Profit7d, stream.Profit30d)
	}
	if len(c.DailyHistory) > 0 {
		total := 0.0
		for _, v := range c.DailyHistory {
			total += v
		}
		avg := total / float64(len(c.DailyHistory))
		s += fmt.Sprintf("Daily Average: $%.2f over %d days\n", avg, len(c.DailyHistory))
	}
	return s
}

func (c *Compounder) recalcTotal() {
	c.recalcProfits()
	total := 0.0
	for _, s := range c.Streams {
		total += s.Balance + s.TotalProfit
	}
	c.TotalCapital = total
	if len(c.DailyHistory) >= 7 {
		recent := c.DailyHistory[len(c.DailyHistory)-7:]
		total7d := 0.0
		for _, v := range recent {
			total7d += v
		}
		c.GrowthRate7d = total7d / 7.0
	}
	if len(c.DailyHistory) >= 30 {
		recent := c.DailyHistory[len(c.DailyHistory)-30:]
		total30d := 0.0
		for _, v := range recent {
			total30d += v
		}
		c.GrowthRate30d = total30d / 30.0
	}
}

func (c *Compounder) ProjectGrowth(days int) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	rate := c.GrowthRate30d
	if rate <= 0 { rate = c.GrowthRate7d }
	if rate <= 0 { return "Not enough data to project growth" }
	projected := c.TotalCapital
	for i := 0; i < days; i++ {
		projected += rate
	}
	return fmt.Sprintf("At current rate ($%.2f/day), in %d days: $%.2f → $%.2f (+$%.2f)",
		rate, days, c.TotalCapital, projected, projected-c.TotalCapital)
}

func (c *Compounder) MarshalJSON() ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	type Alias Compounder
	return json.Marshal(&struct{ *Alias }{Alias: (*Alias)(c)})
}
