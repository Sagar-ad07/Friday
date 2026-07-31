package trading

import (
	"encoding/json"
	"log"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/friday-prototype/friday-go/friday"
)

var ProjectRoot = friday.ProjectRoot

// ──────────────────────────────────────────────────────────────────────
// Grid Trading Strategy — automated buy low, sell high in a range
// 24/7 profit capture from market volatility
// ──────────────────────────────────────────────────────────────────────

type GridLevel struct {
	Index      int     `json:"index"`
	BuyPrice   float64 `json:"buy_price"`
	SellPrice  float64 `json:"sell_price"`
	BuyFilled  bool    `json:"buy_filled"`
	SellFilled bool    `json:"sell_filled"`
	PnL        float64 `json:"pnl"`
}

type GridConfig struct {
	Symbol        string  `json:"symbol"`
	UpperPrice    float64 `json:"upper_price"`    // top of grid
	LowerPrice    float64 `json:"lower_price"`    // bottom of grid
	Grids         int     `json:"grids"`          // number of levels
	Investment    float64 `json:"investment"`     // total USDT to deploy
	TriggerPrice  float64 `json:"trigger_price"`  // price to start at
	StopLossPct   float64 `json:"stop_loss_pct"`  // % below lower to stop
	TakeProfitPct float64 `json:"take_profit_pct"`// % above upper to take profit
}

type GridStrategy struct {
	mu         sync.RWMutex
	Config     GridConfig      `json:"config"`
	Levels     []GridLevel     `json:"levels"`
	Portfolio  *CryptoPortfolio `json:"-"`
	Active     bool            `json:"active"`
	StartTime  time.Time       `json:"start_time"`
	TotalPnL   float64         `json:"total_pnl"`
	TradesDone int             `json:"trades_done"`
	SavePath   string          `json:"-"`
}

func gridSavePath(symbol string) string { return filepath.Join(ProjectRoot, "data", "grid_"+symbol+".json") }

func NewGridStrategy(cfg GridConfig, portfolio *CryptoPortfolio) *GridStrategy {
	// Try to load existing grid state
	sp := gridSavePath(cfg.Symbol)
	os.MkdirAll(filepath.Dir(sp), 0755)
	if data, err := os.ReadFile(sp); err == nil {
		var existing GridStrategy
		if json.Unmarshal(data, &existing) == nil && existing.Active {
			existing.Portfolio = portfolio
			existing.SavePath = sp
			log.Printf("Grid loaded: %s active=%t pnl=$%.2f trades=%d", cfg.Symbol, existing.Active, existing.TotalPnL, existing.TradesDone)
			return &existing
		}
	}

	priceStep := (cfg.UpperPrice - cfg.LowerPrice) / float64(cfg.Grids)
	investmentPerGrid := cfg.Investment / float64(cfg.Grids)

	levels := make([]GridLevel, cfg.Grids)
	for i := 0; i < cfg.Grids; i++ {
		buyPrice := cfg.LowerPrice + priceStep*float64(i)
		sellPrice := cfg.LowerPrice + priceStep*float64(i+1)
		levels[i] = GridLevel{
			Index:     i,
			BuyPrice:  math.Round(buyPrice*10000) / 10000,
			SellPrice: math.Round(sellPrice*10000) / 10000,
			BuyFilled: buyPrice <= cfg.TriggerPrice,
		}
	}

	// Place initial grid buys
	for i := range levels {
		if levels[i].BuyFilled {
			qty := investmentPerGrid / levels[i].BuyPrice
			portfolio.PlaceOrder(cfg.Symbol, "BUY", levels[i].BuyPrice, math.Round(qty*100000)/100000)
		}
	}

	return &GridStrategy{
		Config:    cfg,
		Levels:    levels,
		Portfolio: portfolio,
		Active:    true,
		StartTime: time.Now(),
	}
}

// Tick runs one grid cycle with the current price
func (g *GridStrategy) Tick(currentPrice float64) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.Active {
		return
	}

	// Check stop loss
	if currentPrice < g.Config.LowerPrice*(1-g.Config.StopLossPct/100) {
		log.Printf("[GRID] Stop loss triggered at %.4f (lower=%.4f)", currentPrice, g.Config.LowerPrice)
		g.CloseAll()
		return
	}

	// Check take profit
	if currentPrice > g.Config.UpperPrice*(1+g.Config.TakeProfitPct/100) {
		log.Printf("[GRID] Take profit triggered at %.4f (upper=%.4f)", currentPrice, g.Config.UpperPrice)
		g.CloseAll()
		return
	}

	// Check each grid level
	for i := range g.Levels {
		level := &g.Levels[i]

		// Buy signal: price drops to buy level
		if !level.BuyFilled && currentPrice <= level.BuyPrice {
			level.BuyFilled = true
			qty := (g.Config.Investment / float64(g.Config.Grids)) / level.BuyPrice
			g.Portfolio.PlaceOrder(g.Config.Symbol, "BUY", level.BuyPrice, math.Round(qty*100000)/100000)
			g.TradesDone++
			log.Printf("[GRID] BUY @ %.4f (%d/%d)", level.BuyPrice, i+1, g.Config.Grids)
		}

		g.save()
		// Sell signal: price rises to sell level
		if level.BuyFilled && !level.SellFilled && currentPrice >= level.SellPrice {
			level.SellFilled = true
			qty := (g.Config.Investment / float64(g.Config.Grids)) / level.BuyPrice

			// We need to know how much we bought at this level
			holdingsQty := qty // simplified: we buy same qty at each level
			g.Portfolio.PlaceOrder(g.Config.Symbol, "SELL", level.SellPrice, math.Round(holdingsQty*100000)/100000)

			profit := (level.SellPrice - level.BuyPrice) * holdingsQty
			level.PnL += profit
			g.TotalPnL += profit
			g.TradesDone++
			log.Printf("[GRID] SELL @ %.4f | profit=%.4f USDT", level.SellPrice, profit)
			g.save()
		}
	}
}

func (g *GridStrategy) save() {
	if g.SavePath == "" { return }
	g.mu.RLock()
	defer g.mu.RUnlock()
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil { return }
	os.MkdirAll(filepath.Dir(g.SavePath), 0755)
	os.WriteFile(g.SavePath, data, 0644)
}

// CloseAll liquidates all positions and resets
func (g *GridStrategy) CloseAll() {
	g.Active = false
	for i := range g.Levels {
		level := &g.Levels[i]
		if level.BuyFilled && !level.SellFilled {
			level.SellFilled = true
		}
		level.BuyFilled = false
	}
	log.Printf("[GRID] All positions closed. Total PnL: %.4f USDT", g.TotalPnL)
}

func (g *GridStrategy) Status() map[string]interface{} {
	g.mu.RLock()
	defer g.mu.RUnlock()

	activeLevels := 0
	filledBuys := 0
	filledSells := 0
	for _, l := range g.Levels {
		if l.BuyFilled {
			filledBuys++
		}
		if l.SellFilled {
			filledSells++
		}
		if l.BuyFilled && !l.SellFilled {
			activeLevels++
		}
	}

	return map[string]interface{}{
		"active":         g.Active,
		"symbol":         g.Config.Symbol,
		"range":          []float64{g.Config.LowerPrice, g.Config.UpperPrice},
		"grids":          g.Config.Grids,
		"active_levels":  activeLevels,
		"filled_buys":    filledBuys,
		"filled_sells":   filledSells,
		"trades":         g.TradesDone,
		"total_pnl":      math.Round(g.TotalPnL*10000) / 10000,
		"investment":     g.Config.Investment,
		"running_time":   time.Since(g.StartTime).String(),
	}
}

// ──────────────────────────────────────────────────────────────────────
// Grid Optimizer — finds the best grid config via backtest
// ──────────────────────────────────────────────────────────────────────

type GridOptimizer struct {
	Symbol    string
	Klines    []Kline
	BestP     float64
	BestUpper float64
	BestLower float64
	BestGrids int
}

type GridBacktestResult struct {
	Config      GridConfig `json:"config"`
	TotalPnL    float64    `json:"total_pnl"`
	Trades      int        `json:"trades"`
	WinRate     float64    `json:"win_rate"`
	MaxDrawdown float64    `json:"max_drawdown"`
	SharpeRatio float64    `json:"sharpe_ratio"`
}

func OptimizeGrid(symbol string, klines []Kline) *GridBacktestResult {
	if len(klines) < 100 {
		return nil
	}

	// Find range
	highs := make([]float64, len(klines))
	lows := make([]float64, len(klines))
	for i, k := range klines {
		highs[i] = k.High
		lows[i] = k.Low
	}

	avgPrice := avg(closesFromKlines(klines))
	upper := avgPrice * 1.1
	lower := avgPrice * 0.9

	// Try different grid counts
	bestResult := &GridBacktestResult{TotalPnL: -1e9}
	for grids := 5; grids <= 30; grids += 5 {
		result := backtestGrid(symbol, klines, GridConfig{
			Symbol:     symbol,
			UpperPrice: upper,
			LowerPrice: lower,
			Grids:      grids,
			Investment: 1000,
		})
		if result != nil && result.TotalPnL > bestResult.TotalPnL {
			bestResult = result
		}
	}

	return bestResult
}

func backtestGrid(symbol string, klines []Kline, cfg GridConfig) *GridBacktestResult {
	portfolio := NewCryptoPortfolio(cfg.Investment)
	grid := NewGridStrategy(cfg, portfolio)
	initialEquity := cfg.Investment

	peakEquity := initialEquity
	maxDrawdown := 0.0
	wins := 0
	losses := 0

	for _, k := range klines[1:] {
		grid.Tick(k.Close)
		equity := portfolio.GetEquity(map[string]float64{symbol: k.Close})
		drawdown := (peakEquity - equity) / peakEquity * 100
		if drawdown > maxDrawdown {
			maxDrawdown = drawdown
		}
		if equity > peakEquity {
			peakEquity = equity
		}

		// Track win/loss from trades
		// Simplified: avg price of all trades
	}

	grid.CloseAll()
	finalEquity := portfolio.GetEquity(map[string]float64{symbol: closesFromKlines(klines)[len(klines)-1]})

	return &GridBacktestResult{
		Config:      cfg,
		TotalPnL:    math.Round((finalEquity-initialEquity)*100) / 100,
		Trades:      grid.TradesDone,
		WinRate:     math.Round(float64(wins)/float64(wins+losses+1)*100) / 100,
		MaxDrawdown: math.Round(maxDrawdown*100) / 100,
	}
}

func closesFromKlines(klines []Kline) []float64 {
	c := make([]float64, len(klines))
	for i, k := range klines {
		c[i] = k.Close
	}
	return c
}

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}
