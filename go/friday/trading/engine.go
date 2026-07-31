package trading

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/friday-prototype/friday-go/internal/infrastructure/execution"
	"github.com/friday-prototype/friday-go/friday"
	"github.com/friday-prototype/friday-go/pipeline"
	"github.com/friday-prototype/friday-go/safety"
	"github.com/friday-prototype/friday-go/trading"
	"github.com/gin-gonic/gin"
	gomt5 "github.com/mukbeast4/go-mt5"
)

// Engine wraps the trading system
type Engine struct {
	cfg            *friday.Config
	bot            *trading.TradingBot
	execEngine     *execution.ExecutionEngine
	guardians      *safety.TradingGuardians
	feed           *pipeline.UltraLowLatencyFeed
	router         *gin.Engine
	server         *http.Server
	mt5Client      *gomt5.Client
	mu             sync.RWMutex
	running        bool
	stopCh         chan struct{}

	// Crypto 24/7 trading
	binance        *BinanceClient
	cryptoPortfolio *CryptoPortfolio
	gridStrategy   *GridStrategy
	gridCh         chan float64
	gridActive     bool

	// Max lot override (set via /trading/start API)
	maxLot float64

	// propFirmAccount is true when the connected MT5 server is a prop firm
	// (e.g. "BlueGuardian-Server"). It drives the swing-risk clamp
	// (30-80 pip SL / 1:2 R:R) and the bot-side cap enforcement. Personal
	// accounts (e.g. Exness-MT5Real3) have NO cap and run 24/7 per user.
	propFirmAccount bool
	// Daily PROFIT cap for the prop-firm account ($). Read from
	// PROP_DAILY_PROFIT_CAP at startup (default 37.50). Once realized daily
	// PnL reaches this, no new entries on the prop account until the next
	// day (bot halts + /mt5/order returns 403). Personal accounts: 0 (no cap).
	propDailyProfitCap float64

	// Second MT5 client for the Exness personal account. Connected via a
	// computed pipe name (deterministic SHA256 of the terminal64.exe path).
	// Drives its OWN autonomous TradingBot loop (exnessBot) — same TPCS
	// strategy, no cap, 24/7. Per user directive: hard-stop only when
	// balance drops below AED 10 (LowBalanceNotice, not a trade-blocking
	// stop — just an honest status flag Friday reports).
	mt5ClientExness   *gomt5.Client
	exnessConnected   bool
	exnessTrackedTickets map[int64]bool
	exnessMu          sync.Mutex
	exnessBot         *trading.TradingBot
	exnessLowBalance  bool
	exnessStopCh      chan struct{}
	exnessMonWg       sync.WaitGroup

	// Exness scalper config — fixed micro-lot risk (user directive: 0.01
	// lot, $1 loss / $2 profit ≈ 10-pip SL / 20-pip TP on EURUSDm at
	// $0.10/pip). Tunable via EXNESS_LOT, EXNESS_SL_PIPS, EXNESS_TP_PIPS.
	exnessLot    float64
	exnessSLPips float64
	exnessTPPips float64
}

func NewEngine(cfg *friday.Config) *Engine {
	if !cfg.DevMode {
		gin.SetMode(gin.ReleaseMode)
	}

	e := &Engine{
		cfg:             cfg,
		bot:             trading.NewTradingBot(5000.0, "trading/status_bg.json"),
		execEngine:      execution.NewExecutionEngine(),
		guardians:       safety.NewTradingGuardians(5000.0),
		feed:            pipeline.NewUltraLowLatencyFeed(),
		stopCh:          make(chan struct{}),
		binance:         NewBinanceClient(),
		cryptoPortfolio: NewCryptoPortfolio(1000.0),
		gridCh:          make(chan float64, 100),
	}

	e.setupRouter()
	return e
}

func (e *Engine) setupRouter() {
	e.router = gin.New()
	e.router.Use(gin.Recovery())
	e.router.Use(requestLogger())
	e.router.Use(corsMiddleware())

	// Health
	e.router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy", "mt5_connected": e.mt5Client != nil && e.mt5Client.Connected()})
	})

	// Trading control
	e.router.POST("/trading/start", func(c *gin.Context) {
		var req struct {
			MaxLot float64 `json:"max_lot"`
		}
		c.ShouldBindJSON(&req)
		if req.MaxLot > 0 {
			e.maxLot = req.MaxLot
			log.Printf("Max lot set to %.2f", req.MaxLot)
		}
		if err := e.bot.Start(); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"status": "started"})
	})
	e.router.POST("/trading/stop", func(c *gin.Context) {
		e.bot.Stop()
		c.JSON(200, gin.H{"status": "stopped"})
	})
	e.router.GET("/trading/status", func(c *gin.Context) {
		c.JSON(200, e.bot.GetStatus())
	})
	e.router.POST("/trading/execute", func(c *gin.Context) {
		var req struct {
			Side   string  `json:"side" binding:"required"`
			Lot    float64 `json:"lot"`
			SLPips int     `json:"sl_pips"`
			TPPips int     `json:"tp_pips"`
			RiskUSD float64 `json:"risk_usd"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		if req.Lot <= 0 {
			req.Lot = 0.01
		}
		if req.SLPips <= 0 {
			req.SLPips = 10
		}
		if req.TPPips <= 0 {
			req.TPPips = 20
		}
		if req.RiskUSD <= 0 {
			req.RiskUSD = 1.0
		}
		lotSize := req.Lot
		slPips := req.SLPips
		tpPips := req.TPPips
		c.JSON(200, gin.H{
			"status":  "executed",
			"side":    req.Side,
			"lot":     lotSize,
			"sl_pips": slPips,
			"tp_pips": tpPips,
			"risk_usd": req.RiskUSD,
			"message": fmt.Sprintf("%s %0.2f lots | SL: %dpips | TP: %dpips | Risk: $%.2f", req.Side, lotSize, slPips, tpPips, req.RiskUSD),
		})
	})
	e.router.POST("/trading/close-all", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "closed",
			"message": "All trades closed successfully",
		})
	})

	// MT5 endpoints
	e.router.GET("/mt5/account", e.handleMT5Account)
	e.router.GET("/mt5/positions", e.handleMT5Positions)
	e.router.POST("/mt5/order", e.handleMT5Order)
	e.router.GET("/mt5/tick/:symbol", e.handleMT5Tick)
	e.router.GET("/mt5/rates/:symbol/:timeframe/:count", e.handleMT5Rates)
	// Real SymbolSelect — fixes "no tick available" (MT5 retcode 10019)
	// by subscribing the symbol into the terminal's Market Watch. The agent
	// calls this after explain_mt5_error diagnoses a no-tick failure.
	e.router.POST("/mt5/select/:symbol", e.handleMT5Select)
	// Closed-deal history (last N hours). Same gomt5.HistoryDealsGet call
	// the bot's positionMonitor uses to realize PnL. Exposed here so the
	// agent can answer "what did I close today / did I breach the cap?"
	// without waiting for the monitor to pick up the close.
	e.router.GET("/mt5/history/:hours", e.handleMT5History)

	// Exness personal account (second MT5 client) — 24/7, no cap.
	// All endpoints route to mt5ClientExness, not the primary client.
	e.setupExnessRoutes()

	// AI Decision
	e.router.POST("/ai/decide", func(c *gin.Context) {
		var req struct {
			MarketData map[string]interface{} `json:"market_data"`
			Strategy   string                 `json:"strategy"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		canTrade := e.guardians.CanTrade(0.01, 0.8, 1.5, 60)
		c.JSON(200, gin.H{"can_trade": canTrade, "market_data": req.MarketData, "strategy": req.Strategy})
	})

	// ────── Crypto 24/7 Trading ──────

	// Binance price feed
	e.router.GET("/crypto/price/:symbol", func(c *gin.Context) {
		symbol := c.Param("symbol")
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		price, err := e.binance.GetPrice(ctx, symbol)
		if err != nil {
			c.JSON(502, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"symbol": symbol, "price": price})
	})

	// Binance klines (for charting + backtesting)
	e.router.GET("/crypto/klines/:symbol/:interval/:limit", func(c *gin.Context) {
		symbol := c.Param("symbol")
		interval := c.Param("interval")
		limit := 100
		if c.Param("limit") != "" {
			fmt.Sscanf(c.Param("limit"), "%d", &limit)
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		klines, err := e.binance.GetKlines(ctx, symbol, interval, limit)
		if err != nil {
			c.JSON(502, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"symbol": symbol, "interval": interval, "klines": klines, "count": len(klines)})
	})

	// 24hr ticker
	e.router.GET("/crypto/ticker/:symbol", func(c *gin.Context) {
		symbol := c.Param("symbol")
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		ticker, err := e.binance.Get24hrTicker(ctx, symbol)
		if err != nil {
			c.JSON(502, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{
			"symbol": ticker.Symbol, "price": ticker.Price,
			"change_24h": ticker.PriceChange, "volume_24h": ticker.Volume24,
			"high_24h": ticker.High24, "low_24h": ticker.Low24,
		})
	})

	// Portfolio status
	e.router.GET("/crypto/portfolio", func(c *gin.Context) {
		summary := e.cryptoPortfolio.GetSummary()
		prices := make(map[string]float64)
		for sym := range e.cryptoPortfolio.Holdings {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
			defer cancel()
			if price, err := e.binance.GetPrice(ctx, sym); err == nil {
				prices[sym] = price
			}
		}
		equity := e.cryptoPortfolio.GetEquity(prices)
		summary["equity"] = math.Round(equity*100) / 100
		summary["holdings_detail"] = prices
		c.JSON(200, summary)
	})

	// Trade crypto (paper only)
	e.router.POST("/crypto/trade", func(c *gin.Context) {
		var req struct {
			Symbol   string  `json:"symbol" binding:"required"`
			Side     string  `json:"side" binding:"required,oneof=BUY SELL"`
			Price    float64 `json:"price"`
			Quantity float64 `json:"quantity" binding:"required,gt=0"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}

		// Use latest price if not specified
		price := req.Price
		if price <= 0 {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
			defer cancel()
			if p, err := e.binance.GetPrice(ctx, req.Symbol); err == nil {
				price = p
			}
		}
		if price <= 0 {
			c.JSON(400, gin.H{"error": "could not determine price"})
			return
		}

		order := e.cryptoPortfolio.PlaceOrder(req.Symbol, req.Side, price, req.Quantity)
		c.JSON(200, order)
	})

	// ────── Grid Trading ──────

	e.router.POST("/grid/start", func(c *gin.Context) {
		var req GridConfig
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		if req.Grids <= 0 {
			req.Grids = 10
		}
		if req.Investment <= 0 {
			req.Investment = 500
		}

		// Get current price for trigger
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		triggerPrice, err := e.binance.GetPrice(ctx, req.Symbol)
		if err != nil {
			c.JSON(502, gin.H{"error": fmt.Sprintf("cannot get price: %v", err)})
			return
		}
		if req.LowerPrice <= 0 {
			req.LowerPrice = triggerPrice * 0.95
		}
		if req.UpperPrice <= 0 {
			req.UpperPrice = triggerPrice * 1.05
		}
		req.TriggerPrice = triggerPrice

		e.mu.Lock()
		e.gridStrategy = NewGridStrategy(req, e.cryptoPortfolio)
		e.gridActive = true
		e.mu.Unlock()

		// Start background price watcher
		go e.gridWatcher(req.Symbol)

		c.JSON(200, gin.H{"status": "grid started", "config": req, "levels": len(e.gridStrategy.Levels)})
	})

	e.router.POST("/grid/stop", func(c *gin.Context) {
		e.mu.Lock()
		if sg := e.gridStrategy; sg != nil {
			sg.CloseAll()
		}
		e.gridActive = false
		e.mu.Unlock()
		c.JSON(200, gin.H{"status": "grid stopped"})
	})

	e.router.GET("/grid/status", func(c *gin.Context) {
		e.mu.RLock()
		defer e.mu.RUnlock()
		if e.gridStrategy == nil {
			c.JSON(200, gin.H{"active": false, "message": "no grid running"})
			return
		}
		c.JSON(200, e.gridStrategy.Status())
	})

	// ────── Market Regime ──────

	e.router.GET("/regime/:symbol/:interval", func(c *gin.Context) {
		symbol := c.Param("symbol")
		interval := c.Param("interval")
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		klines, err := e.binance.GetKlines(ctx, symbol, interval, 100)
		if err != nil {
			c.JSON(502, gin.H{"error": err.Error()})
			return
		}
		regime := DetectRegime(klines)
		signal := GenerateSignal(symbol, klines, e.cryptoPortfolio)
		c.JSON(200, gin.H{
			"regime": regime,
			"signal": signal,
		})
	})

	// ────── Backtesting ──────

	e.router.POST("/backtest/run", func(c *gin.Context) {
		var req BacktestConfig
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		if req.Symbol == "" {
			req.Symbol = "BTCUSDT"
		}
		if req.Interval == "" {
			req.Interval = "1h"
		}
		if req.InitialCapital <= 0 {
			req.InitialCapital = 1000
		}

		// Run backtest asynchronously (respects engine stop)
		go func() {
			select {
			case <-e.stopCh:
				return
			default:
			}

			// Fetch klines
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			klines, err := e.binance.GetKlines(ctx, req.Symbol, req.Interval, 500)
			if err != nil {
				log.Printf("[BACKTEST] fetch failed: %v", err)
				return
			}

			// Run all strategies
			strategies := map[string]StrategyFunc{
				"adaptive":       AdaptiveStrategy,
				"mean_reversion": MeanReversionStrategy,
			}

			results := CompareStrategies(req, klines, strategies)
			log.Printf("[BACKTEST] %s %s: %d strategies compared", req.Symbol, req.Interval, len(results))
			for _, r := range results {
				log.Printf("[BACKTEST] %s: PnL=%.2f, trades=%d, win=%.1f%%",
					r.Name, r.Result.TotalPnL, r.Result.Trades, r.Result.WinRate)
			}

			// Grid optimization
			gridResult := OptimizeGrid(req.Symbol, klines)

			log.Printf("[BACKTEST] Grid optimized: PnL=%.2f, grids=%d",
				gridResult.TotalPnL, gridResult.Config.Grids)
		}()

		c.JSON(200, gin.H{"status": "backtest started", "symbol": req.Symbol, "interval": req.Interval})
	})

	// Quick backtest (synchronous, simpler)
	e.router.GET("/backtest/quick/:symbol/:interval", func(c *gin.Context) {
		symbol := c.Param("symbol")
		interval := c.Param("interval")

		ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
		defer cancel()
		klines, err := e.binance.GetKlines(ctx, symbol, interval, 200)
		if err != nil {
			c.JSON(502, gin.H{"error": err.Error()})
			return
		}

		cfg := BacktestConfig{
			Symbol:         symbol,
			Interval:       interval,
			InitialCapital: 1000,
		}

		strategy := func(k []Kline, cp float64, p *CryptoPortfolio) *TradeSignal {
			return GenerateSignal(symbol, k, p)
		}
		result := RunBacktest(cfg, klines, strategy)

		c.JSON(200, result)
	})

	// ────── Forex Backtest (EURUSD All 3 Strategies) ──────

	e.router.GET("/backtest/forex", func(c *gin.Context) {
		dataDir := e.cfg.DataDir
		if dataDir == "" {
			dataDir = "."
		}
		cachePath := filepath.Join(dataDir, "cache", "eurusd_m1.csv")
		days := 7
		if d := c.Query("days"); d != "" {
			fmt.Sscanf(d, "%d", &days)
		}

		candles, err := trading.LoadOrFetchCandles(cachePath, days)
		if err != nil {
			c.JSON(502, gin.H{"error": err.Error()})
			return
		}

		results := trading.RunAllStrategies(candles, 5000.0)

		type strategySummary struct {
			Trades         int     `json:"trades"`
			WinRate        float64 `json:"win_rate"`
			ProfitFactor   float64 `json:"profit_factor"`
			TotalPnL       float64 `json:"total_pnl"`
			MaxDrawdownPct float64 `json:"max_drawdown_pct"`
			Expectancy     float64 `json:"expectancy"`
			Sharpe         float64 `json:"sharpe"`
		}
		summaries := make(map[string]*strategySummary)
		best := ""
		bestPnL := -1e9
		names := make([]string, 0, len(results))
		for name, r := range results {
			names = append(names, name)
			summaries[name] = &strategySummary{
				Trades: r.Trades, WinRate: r.WinRate,
			ProfitFactor: r.ProfitFactor, TotalPnL: r.TotalProfit,
			MaxDrawdownPct: r.MaxDrawdownPct, Expectancy: r.Expectancy, Sharpe: r.Sharpe,
			}
			if r.TotalProfit > bestPnL {
				bestPnL = r.TotalProfit
				best = name
			}
		}

		c.JSON(200, gin.H{
			"symbol":  "EURUSD",
			"candles": len(candles),
			"date_range": gin.H{
				"from": candles[0].Time.Format("2006-01-02 15:04"),
				"to":   candles[len(candles)-1].Time.Format("2006-01-02 15:04"),
			},
			"strategies": summaries,
			"best":       best,
		})
	})

	// ────── Strategy Comparison ──────

	e.router.POST("/strategies/compare", func(c *gin.Context) {
		var req struct {
			Symbol   string `json:"symbol"`
			Interval string `json:"interval"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		if req.Symbol == "" {
			req.Symbol = "BTCUSDT"
		}
		if req.Interval == "" {
			req.Interval = "1h"
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
		defer cancel()
		klines, err := e.binance.GetKlines(ctx, req.Symbol, req.Interval, 500)
		if err != nil {
			c.JSON(502, gin.H{"error": err.Error()})
			return
		}

		cfg := BacktestConfig{
			Symbol:         req.Symbol,
			Interval:       req.Interval,
			InitialCapital: 1000,
		}

		strategies := map[string]StrategyFunc{
			"adaptive":       AdaptiveStrategy,
			"mean_reversion": MeanReversionStrategy,
		}
		results := CompareStrategies(cfg, klines, strategies)

		// Find best
		best := ""
		bestPnL := -1e9
		for _, r := range results {
			if r.Result.TotalPnL > bestPnL {
				best = r.Name
				bestPnL = r.Result.TotalPnL
			}
		}

		c.JSON(200, gin.H{
			"results": results,
			"best":    best,
			"note":    "Run /backtest/run async for complete backtest with grid optimization",
		})
	})

	// ────── Analysis Endpoints ──────

	e.router.GET("/analysis/multitf/:symbol", func(c *gin.Context) {
		symbol := c.Param("symbol")
		tfs := c.DefaultQuery("timeframes", "15m,1h,4h")
		minAgree := 0.66
		fmt.Sscanf(c.DefaultQuery("min_agreement", "0.66"), "%f", &minAgree)

		tfList := strings.Split(tfs, ",")
		result := AnalyzeMultiTF(symbol, tfList, e.cryptoPortfolio, minAgree)
		c.JSON(200, result)
	})

	e.router.GET("/analysis/momentum/:symbol/:interval", func(c *gin.Context) {
		symbol := c.Param("symbol")
		interval := c.Param("interval")
		if interval == "" {
			interval = "1h"
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		klines, err := e.binance.GetKlines(ctx, symbol, interval, 100)
		if err != nil {
			c.JSON(502, gin.H{"error": err.Error()})
			return
		}
		momentum := AnalyzeMomentum(klines)
		c.JSON(200, momentum)
	})

	e.router.GET("/analysis/kelly/:symbol/:interval", func(c *gin.Context) {
		symbol := c.Param("symbol")
		interval := c.Param("interval")
		if interval == "" {
			interval = "1h"
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
		defer cancel()
		klines, err := e.binance.GetKlines(ctx, symbol, interval, 200)
		if err != nil {
			c.JSON(502, gin.H{"error": err.Error()})
			return
		}

		cfg := BacktestConfig{Symbol: symbol, Interval: interval, InitialCapital: 1000}
		result := RunBacktest(cfg, klines, func(k []Kline, cp float64, p *CryptoPortfolio) *TradeSignal {
			return GenerateSignal(symbol, k, p)
		})

		avgWin := result.AvgWin
		avgLoss := result.AvgLoss
		winRate := result.WinRate / 100

		// Convert absolute PnL to reward:risk ratio
		avgWinRatio := 1.5
		avgLossRatio := 1.0
		if avgLoss > 0 && avgWin > 0 {
			avgWinRatio = avgWin / avgLoss
		}

		kelly := CalculateKelly(winRate, avgWinRatio, avgLossRatio)
		c.JSON(200, gin.H{
			"symbol":         symbol,
			"interval":       interval,
			"trade_stats":    gin.H{"total_trades": result.Trades, "win_rate": result.WinRate, "avg_win": avgWin, "avg_loss": avgLoss},
			"kelly_position_sizing": kelly,
			"recommendation": fmt.Sprintf("Risk %.2f%% of portfolio per trade (half-Kelly)", kelly.HalfKelly),
		})
	})

	// ────── Trade Journal ──────

	e.router.GET("/journal/stats", func(c *gin.Context) {
		journal := NewTradeJournal(friday.ProjectRoot)
		stats := journal.Stats()
		c.JSON(200, stats)
	})

	// ────── Optimizer ──────

	e.router.POST("/optimizer/run", func(c *gin.Context) {
		var req OptimizerConfig
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		if req.Symbol == "" {
			req.Symbol = "BTCUSDT"
		}

		results := RunOptimizer(req)
		if results == nil {
			c.JSON(500, gin.H{"error": "optimizer failed — check logs"})
			return
		}

		c.JSON(200, gin.H{
			"symbol":      req.Symbol,
			"combinations_tested": len(results),
			"best_config": results[0],
			"top_10":      results,
		})
	})

	e.router.GET("/optimizer/quick/:symbol", func(c *gin.Context) {
		symbol := c.Param("symbol")
		results := RunOptimizer(OptimizerConfig{
			Symbol:   symbol,
			Interval: "1h",
			Capital:  1000,
		})
		if results == nil {
			c.JSON(500, gin.H{"error": "optimizer failed"})
			return
		}
		c.JSON(200, gin.H{
			"symbol":      symbol,
			"best_config": results[0],
			"top_10":      results,
		})
	})

	// ────── Debug: Bot Intelligence State ──────

	e.router.GET("/debug/market", func(c *gin.Context) {
		status := e.bot.GetStatus()
		c.JSON(200, gin.H{
			"regime":     status["last_regime"],
			"signal":     status["last_signal"],
			"confidence": status["last_confidence"],
			"market_analysis": "see /debug/intel for full analysis",
		})
	})

	e.router.GET("/debug/intel", func(c *gin.Context) {
		f := e.bot.Fusion()
		if f == nil {
			c.JSON(200, gin.H{"fusion": nil, "note": "No fusion decision yet — bot may still be warming up"})
			return
		}
		c.JSON(200, gin.H{
			"direction":    f.Direction,
			"confidence":   f.Confidence,
			"entry_price":  f.EntryPrice,
			"stop_loss":    f.StopLoss,
			"take_profit":  f.TakeProfit,
			"regime":       f.Regime.String(),
			"risk_score":   f.RiskScore,
			"optimal_size": f.OptimalSize,
			"confluences":  f.Confluences,
			"reasoning":    f.Reasoning,
		})
	})

	e.server = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", e.cfg.Host, e.cfg.GetTradingEnginePort()),
		Handler:      e.router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
}

func (e *Engine) Start() error {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return nil
	}
	e.running = true
	e.mu.Unlock()

	e.server = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", e.cfg.Host, e.cfg.GetTradingEnginePort()),
		Handler:      e.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Auto-start MT5 if not running
	cmd := exec.Command("tasklist", "/fi", "imagename eq terminal64.exe")
	if out, _ := cmd.Output(); !strings.Contains(strings.ToLower(string(out)), "terminal64.exe") {
		log.Printf("MT5 not running — attempting to start...")
		// Try common installation paths
		paths := []string{
			`C:\Program Files\MetaTrader 5\terminal64.exe`,
			`C:\Program Files (x86)\MetaTrader 5\terminal64.exe`,
		}
		for _, p := range paths {
			if _, err := os.Stat(p); err == nil {
				exec.Command("cmd", "/C", "start", "", p).Start()
				log.Printf("Started MT5 from %s", p)
				break
			}
		}
		time.Sleep(5 * time.Second)
	}

	// Try MT5 connection (supports MT5_PIPE env var for second terminal)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	var client *gomt5.Client
	var mt5Err error
	if pipe := os.Getenv("MT5_PIPE"); pipe != "" {
		client, mt5Err = gomt5.NewClient(ctx, gomt5.WithPipeName(pipe), gomt5.WithTimeout(5*time.Second))
	} else {
		client, mt5Err = gomt5.NewClient(ctx, gomt5.WithTimeout(5*time.Second))
	}
	cancel()
	if mt5Err == nil {
		e.mt5Client = client
		log.Printf("MT5 connected successfully!")
	} else {
		log.Printf("MT5 connect failed: %v — paper mode", mt5Err)
	}

	// Start MT5 watchdog — reconnects if MT5 is killed
	go func() {
		tick := time.NewTicker(30 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-e.stopCh:
				return
			case <-tick.C:
				if e.mt5Client == nil || !e.mt5Client.Connected() {
					log.Printf("MT5 watchdog: reconnecting...")
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					var client *gomt5.Client
					var err error
					if pipe := os.Getenv("MT5_PIPE"); pipe != "" {
						client, err = gomt5.NewClient(ctx, gomt5.WithPipeName(pipe), gomt5.WithTimeout(5*time.Second))
					} else {
						client, err = gomt5.NewClient(ctx, gomt5.WithTimeout(5*time.Second))
					}
					cancel()
					if err == nil {
						e.mt5Client = client
						log.Printf("MT5 watchdog: reconnected!")
					}
				}
			}
		}
	}()

	// Connect the Exness personal-account MT5 client (second terminal at
	// D:\MetaTrader 5\terminal64.exe). No autonomous bot loop runs on it
	// — Friday places trades via exness_* tools on demand. The monitor
	// goroutine logs realized PnL honestly; no cap, 24/7 per user.
	go e.connectExnessClient()

	// Start HTTP server
	go func() {
		log.Printf("Trading Engine on %s:%d", e.cfg.Host, e.cfg.GetTradingEnginePort())
		if err := e.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Trading server error: %v", err)
		}
	}()

	// Wire bot with MT5 execution
	e.bot.SetTradeExecutor(func(symbol, direction string, volume, sl, tp float64) (bool, string) {
		if e.mt5Client == nil || !e.mt5Client.Connected() {
			return false, "MT5 not connected"
		}
		// Cap volume at maxLot if set
		if e.maxLot > 0 && volume > e.maxLot {
			log.Printf("Capping volume from %.3f to %.2f (maxLot setting)", volume, e.maxLot)
			volume = e.maxLot
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// Must subscribe the symbol into Market Watch before ticks will
		// flow — without this SymbolInfoTick fails with "no tick available"
		// (retcode 10019), which was the bot's pre-P0 chronic state.
		if err := e.mt5Client.SymbolSelect(ctx, symbol, true); err != nil {
			return false, fmt.Sprintf("select %s: %v", symbol, err)
		}
		orderType := gomt5.OrderTypeBuy
		price := 0.0
		tick, err := e.mt5Client.SymbolInfoTick(ctx, symbol)
		if err != nil {
			return false, "tick: " + err.Error()
		}
		if direction == "SELL" {
			orderType = gomt5.OrderTypeSell
			price = tick.Bid
		} else {
			price = tick.Ask
		}

		// DAILY PROFIT CAP (propfirm): the bot's own executor refuses to
		// open when broker-truth realized PnL already hit the cap — the
		// in-memory DailyPNL can lag behind after a restart, so the broker
		// deals are the final authority here too.
		if reason := e.propFirmEntryBlockReason(); reason != "" {
			log.Printf("BOT ENTRY BLOCKED: %s", reason)
			return false, reason
		}

		// SWING RISK CLAMP (P3): on propfirm accounts the strategy's ATR-based
		// SL/TP must obey swing bounds — SL ∈ [30, 80] pips, TP = 2× SL.
		// This prevents an aggressive trend-pullback trade from accidentally
		// becoming a scalp (15-pip SL) or a position hold (100-pip) that
		// would violate the propfirm's swing-only mandate._tp stays the
		// strategy's value if it's >= 2× SL (lets TP breathe wider).
		// Personal accounts (Exness) keep the strategy's unclamped SL/TP.
		if e.propFirmAccount {
			pip := 0.0001
			slDist := math.Abs(sl - price)
			// Clamp SL to [30 pips, 80 pips] for swing-only compliance.
			minSL := 30 * pip
			maxSL := 80 * pip
			if slDist < minSL {
				slDist = minSL
				log.Printf("Swing clamp (propfirm): SL widened to 30 pips from %.1f", math.Abs(sl-price)/pip)
			}
			if slDist > maxSL {
				slDist = maxSL
				log.Printf("Swing clamp (propfirm): SL narrowed to 80 pips from %.1f", math.Abs(sl-price)/pip)
			}
			tpDist := math.Abs(tp - price)
			minTP := 2 * slDist // 1:2 R:R minimum per propfirm swing rules
			if tpDist < minTP {
				tpDist = minTP
			}
			if direction == "BUY" {
				sl = price - slDist
				tp = price + tpDist
			} else {
				sl = price + slDist
				tp = price - tpDist
			}
			log.Printf("Swing clamp (propfirm): dir=%s entry=%.5f SL=%.5f (%.1fp) TP=%.5f (%.1fp) R:R=%.2f",
				direction, price, sl, slDist/pip, tp, tpDist/pip, tpDist/slDist)
		}

		// DAILY PROFIT CAP (propfirm): tighten the TP so a winning trade can
		// never realize more than the daily profit cap (default $37.50) —
		// lockProfit = cap - $1.00 buffer for swap/commission. The broker
		// closes the position at exactly this level: no overshoot, works
		// even if the engine dies. Personal accounts (Exness) keep their TP.
		tp = e.clampProfitTP(direction, price, tp, volume)

		result, err := e.mt5Client.OrderSend(ctx, gomt5.TradeRequest{
			Action: gomt5.TradeActionDeal, Symbol: symbol,
			Volume: volume, Type: orderType, Price: price,
			Deviation: 20, SL: sl, TP: tp,
			TypeFilling: gomt5.OrderFillingIOC,
		})
		if err != nil {
			return false, err.Error()
		}
		if result.Retcode != 10009 && result.Retcode != 0 {
			return false, fmt.Sprintf("retcode %d", result.Retcode)
		}
		return true, fmt.Sprintf("order=%d deal=%d price=%.5f", result.Order, result.Deal, result.Price)
	})

	// Wire bot with MT5 price feed (or simulated fallback)
	e.bot.SetPriceFeed(func(symbol string, count int) ([]trading.Candle, error) {
		if e.mt5Client != nil && e.mt5Client.Connected() {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			// Always subscribe the symbol first — even on a prop firm where
			// a non-tradable suffix like "EURUSDm" was the historical cause
			// of "no tick available" (retcode 10019).
			_ = e.mt5Client.SymbolSelect(ctx, symbol, true)
			// P3: switch to H1 candles (Trend Pullback strategy needs EMA200
			// which needs ~200 H1 candles = ~8 days of history, very
			// reasonable). Count of 300 requested gives the strategy
			// enough history to compute EMA200 reliably.
			need := count
			if need < 300 {
				need = 300
			}
			rates, err := e.mt5Client.CopyRatesFromPos(ctx, symbol, gomt5.TimeframeH1, 0, need)
			if err == nil && len(rates) > 0 {
				candles := make([]trading.Candle, len(rates))
				for i, r := range rates {
					candles[i] = trading.Candle{
						Time:   time.Unix(r.Time, 0).UTC(),
						Open:   r.Open, High: r.High,
						Low:    r.Low, Close: r.Close,
						Volume: float64(r.TickVolume),
					}
				}
				return candles, nil
			}
		}
		// LIVE ONLY: never fabricate candles. If MT5 is unreachable the bot
		// gets no feed and cannot produce a signal — no synthetic fallback.
		return nil, fmt.Errorf("MT5 not connected - no price feed (live-only mode)")
	})

	// Wire bot with the MT5 position monitor so realized PnL flows back into
	// BotState.DailyPNL / TotalPNL / Wins / Losses. Without this, the daily
	// loss cap is never enforced on real results — the bot would happily
	// accumulate losses past the $150 limit since checkRules() reads a
	// DailyPNL that never moves.
	e.bot.SetPositionMonitor(
		func() ([]trading.OpenPosition, error) {
			if e.mt5Client == nil || !e.mt5Client.Connected() {
				return nil, fmt.Errorf("MT5 not connected")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			positions, err := e.mt5Client.PositionsGet(ctx, nil)
			if err != nil {
				return nil, err
			}
			out := make([]trading.OpenPosition, 0, len(positions))
			for _, p := range positions {
				dir := "sell"
				if p.Type == gomt5.PositionTypeBuy {
					dir = "buy"
				}
				out = append(out, trading.OpenPosition{
					Ticket:       p.Ticket,
					Symbol:       p.Symbol,
					Type:         dir,
					Volume:       p.Volume,
					PriceOpen:    p.PriceOpen,
					PriceCurrent: p.PriceCurrent,
					Profit:       p.Profit,
					SL:           p.PriceSL,
					TP:           p.PriceTP,
				})
			}
			return out, nil
		},
		func(ticket int64) (float64, bool, error) {
			if e.mt5Client == nil || !e.mt5Client.Connected() {
				return 0, false, fmt.Errorf("MT5 not connected")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			// Fetch deals for this specific position ticket directly —
			// cleaner than a time-window scan, works across DST/day boundaries.
			deals, err := e.mt5Client.HistoryDealsGet(ctx, &gomt5.HistoryFilter{Ticket: ticket})
			if err != nil {
				return 0, false, err
			}
			var foundProfit float64
			found := false
			for _, d := range deals {
				// Skip entry deals (DEAL_ENTRY_IN, Profit=0). Only closure
				// deals (OUT / INOUT) carry the realized PnL.
				if d.Entry == gomt5.DealEntryOut || d.Entry == gomt5.DealEntryInOut {
					foundProfit += d.Profit + d.Swap + d.Commission
					found = true
				}
			}
			return foundProfit, found, nil
		},
	)

	// Start bot in background (with panic recovery)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Engine panic: %v — restarting bot", r)
				time.Sleep(5 * time.Second)
				go e.bot.Start()
			}
		}()
		e.bot.Start()
	}()

	// Wire the PropFirm recorder AFTER bot is started. When positionMonitor
	// realizes PnL from a closed ticket it pushes through this callback →
	// PropFirmState.RecordTrade updates daily_pnl/total_pnl/violations,
	// persists to data/propfirm.json, and returns the violation (if any)
	// — which the bot uses as a sticky halt signal. This is the bridge
	// between the trade-realization layer in trading/ and the compliance
	// layer in friday/.
	// Cap config — derived from the connected MT5 server name.
	// BlueGuardian-Server = the $5k propfirm with $150 daily loss cap,
	// 5% max drawdown, $250 profit target, 15%% consistency (all already
	// seeded into PropFirmConfig in propfirm.go).
	// Exness-MT5Real3 = personal account — no profit limit, run 24/7
	// per user directive. PropFirmState compliance is only enforced for
	// propfirm accounts; for personal accounts RecordTrade is a no-op
	// so we don't accidentally pollute the BG compliance state.
	serverName := ""
	acctCtx, acctCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if info, err := e.mt5Client.AccountInfo(acctCtx); err == nil {
		serverName = info.Server
	}
	acctCancel()
	isPropFirm := strings.Contains(strings.ToLower(serverName), "blueguardian")
	e.propFirmAccount = isPropFirm

	// Daily profit cap: $37.50 by default, overridable via .env.
	e.propDailyProfitCap = 0
	if isPropFirm {
		e.propDailyProfitCap = 37.50
		if v := os.Getenv("PROP_DAILY_PROFIT_CAP"); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
				e.propDailyProfitCap = f
			}
		}
	}

	if isPropFirm {
		e.bot.SetCapConfig(trading.CapConfig{Enabled: true, Limit: 150.0, ProfitLimit: e.propDailyProfitCap})
		e.bot.SetRecordTrade(func(pnl float64) (bool, string) {
			pf := friday.GetPropFirm()
			allowed, violation := pf.RecordTrade(pnl)
			return allowed, violation
		})

		// Wire the trade ledger to record every closed trade to SQLite
		e.bot.SetLedger(func(ticket int64, symbol, direction string, lots, openPrice, closePrice float64, pnl float64) {
			friday.RecordTradeToLedger(ticket, symbol, direction, lots, openPrice, closePrice, time.Now(), time.Now(), pnl)
		})
		e.bot.SetStrategyUpdater(func(emaFast, emaSlow, emaPullback, rsiPeriod, atrPeriod int, slMult, tpMult float64) {
			e.bot.ApplyStrategyParams(emaFast, emaSlow, emaPullback, rsiPeriod, atrPeriod, slMult, tpMult)
		})
		friday.SetStrategyApplyCallback(func(emaFast, emaSlow, emaPullback, rsiPeriod, atrPeriod int, slMult, tpMult float64) {
			e.bot.ApplyStrategyParams(emaFast, emaSlow, emaPullback, rsiPeriod, atrPeriod, slMult, tpMult)
		})

		// RE-SYNC with broker truth: a restart loses tracked tickets, so the
		// in-memory DailyPNL can understate today's realized PnL. Pull the
		// day's closed deals from MT5 and fold them into the bot state + the
		// propfirm compliance tracker. This is what keeps the $37.50 cap
		// (and the $150 loss cap) enforced across restarts.
		if realized := e.propFirmRealizedToday(); realized != 0 {
			if realized > e.bot.GetDailyPNL() {
				e.bot.SetDailyPNL(realized)
				log.Printf("PropFirm sync: today's realized PnL $%.2f folded into bot state", realized)
			}
			pf := friday.GetPropFirm()
			if pf.TotalPnL == 0 && pf.DailyPnL == 0 {
				if _, violation := pf.RecordTrade(realized); violation != "" {
					log.Printf("PropFirm sync: compliance violation on restore: %s", violation)
				} else {
					log.Printf("PropFirm sync: compliance ledger updated with $%.2f", realized)
				}
			}
		}

		log.Printf("Cap config: BlueGuardian propfirm - $150 daily loss cap enforced (compliance: 5%% DD, $250 target, 15%% consistency via PropFirmState.RecordTrade), daily PROFIT cap $%.2f", e.propDailyProfitCap)
	} else {
		e.bot.SetCapConfig(trading.CapConfig{Enabled: false, Limit: 0})
		e.bot.SetRecordTrade(func(pnl float64) (bool, string) {
			// Personal account - no compliance tracking, no cap.
			// Just log so PnL realization is observable in the engine log.
			log.Printf("Personal account: realized pnl $%.2f (no compliance cap, no profit limit)", pnl)
			return true, ""
		})
		log.Printf("Cap config: server=%q is NOT a propfirm - no bot-side daily loss cap (personal account, 24/7 trading per user directive)", serverName)
	}

	// MT5 disabled - no reconnection loop

	// Profit lock: independent watcher that auto-closes positions when
	// floating profit reaches the daily cap (prop firm only).
	go e.profitLockLoop()

	return nil
}

func (e *Engine) Stop(ctx context.Context) {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return
	}
	e.running = false
	close(e.stopCh)
	e.mu.Unlock()

	e.bot.Stop()

	if e.mt5Client != nil {
		e.mt5Client.Close()
	}

	if e.server != nil {
		e.server.Shutdown(ctx)
	}
}

// gridWatcher polls Binance price and runs grid strategy ticks
func (e *Engine) gridWatcher(symbol string) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		e.mu.RLock()
		active := e.gridActive
		grid := e.gridStrategy
		e.mu.RUnlock()

		if !active || grid == nil {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		price, err := e.binance.GetPrice(ctx, symbol)
		cancel()
		if err != nil {
			log.Printf("[GRID WATCHER] price fetch: %v", err)
			continue
		}

		grid.Tick(price)
	}
}

// profitLockLoop watches the prop-firm account's FLOATING profit and
// guarantees the daily profit cap is never crossed:
//  1. ARM (floating >= cap-5): modifies each position's TP at the BROKER
//     to the lock level (cap - $1.00 buffer) — MT5 closes the position
//     at exactly that price, so even a market gap or a dead engine can't
//     overshoot the cap.
//  2. FIRE (floating >= cap): closes every position immediately as a
//     final safety net (e.g. entries made without a TP).
// After a close, realized PnL lands at ~the cap and the bot's own
// profit-cap check + the explicit Stop keep it halted for the day.
// Runs on its own ticker, independent of the bot. Personal accounts
// (Exness) never enter this loop.
func (e *Engine) profitLockLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if !e.propFirmAccount || e.propDailyProfitCap <= 0 {
			continue
		}
		if e.mt5Client == nil || !e.mt5Client.Connected() {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		positions, err := e.mt5Client.PositionsGet(ctx, nil)
		if err != nil || len(positions) == 0 {
			cancel()
			continue
		}
		total := 0.0
		for _, p := range positions {
			total += p.Profit
		}

		// ARM: broker-level TP at the lock level while there's still room.
		if total < e.propDailyProfitCap && total >= e.propDailyProfitCap-5.0 {
			for _, p := range positions {
				if p.Profit < e.propDailyProfitCap-5.0 {
					continue
				}
				lockDist := (e.propDailyProfitCap - 1.0) / (p.Volume * 100000)
				lockTP := p.PriceOpen + lockDist
				if p.Type == gomt5.PositionTypeSell {
					lockTP = p.PriceOpen - lockDist
				}
				curTP := p.PriceTP
				alreadyLocked := p.Type == gomt5.PositionTypeBuy && curTP > 0 && curTP <= lockTP+1e-8 ||
					p.Type == gomt5.PositionTypeSell && curTP > 0 && curTP >= lockTP-1e-8
				if !alreadyLocked {
					res, oerr := e.mt5Client.OrderSend(ctx, gomt5.TradeRequest{
						Action:   gomt5.TradeActionSLTP,
						Symbol:   p.Symbol,
						Position: p.Ticket,
						SL:       p.PriceSL,
						TP:       lockTP,
					})
					if oerr != nil {
						log.Printf("PROFIT LOCK: TP modify #%d failed: %v", p.Ticket, oerr)
						continue
					}
					log.Printf("PROFIT LOCK: #%d TP modified to %.5f (locks +$%.2f, retcode %d)",
						p.Ticket, lockTP, e.propDailyProfitCap-1.0, res.Retcode)
				}
			}
		}

		// FIRE: floating reached the cap — close everything right now.
		if total < e.propDailyProfitCap {
			cancel()
			continue
		}
		log.Printf("PROFIT LOCK: floating +$%.2f >= cap $%.2f — closing %d position(s)", total, e.propDailyProfitCap, len(positions))
		for _, p := range positions {
			tick, terr := e.mt5Client.SymbolInfoTick(ctx, p.Symbol)
			if terr != nil {
				log.Printf("PROFIT LOCK: tick for #%d failed: %v", p.Ticket, terr)
				continue
			}
			closeType := gomt5.OrderTypeBuy
			price := tick.Ask
			if p.Type == gomt5.PositionTypeBuy {
				closeType = gomt5.OrderTypeSell
				price = tick.Bid
			}
			res, oerr := e.mt5Client.OrderSend(ctx, gomt5.TradeRequest{
				Action:      gomt5.TradeActionDeal,
				Symbol:      p.Symbol,
				Volume:      p.Volume,
				Type:        closeType,
				Price:       price,
				Position:    p.Ticket,
				Deviation:   20,
				TypeFilling: gomt5.OrderFillingIOC,
			})
			if oerr != nil {
				log.Printf("PROFIT LOCK: close #%d failed: %v", p.Ticket, oerr)
				continue
			}
			log.Printf("PROFIT LOCK: closed #%d %s %s %.2f @ %.5f pnl=+$%.2f",
				p.Ticket, p.Symbol, positionTypeString(p.Type), p.Volume, res.Price, p.Profit)
		}
		cancel()
		e.bot.Stop()
		log.Printf("PROFIT LOCK: bot stopped for the day (daily profit cap $%.2f)", e.propDailyProfitCap)
	}
}

func (e *Engine) StartTrading() error {
	return e.bot.Start()
}

func (e *Engine) StopTrading() {
	e.bot.Stop()
}

func (e *Engine) GetStatus() map[string]interface{} {
	return e.bot.GetStatus()
}

// MT5 Handlers
func (e *Engine) handleMT5Account(c *gin.Context) {
	if e.mt5Client == nil || !e.mt5Client.Connected() {
		c.JSON(503, gin.H{"error": "MT5 not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	info, err := e.mt5Client.AccountInfo(ctx)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{
		"login":    info.Login, "server": info.Server, "balance": info.Balance,
		"equity": info.Equity, "currency": info.Currency, "leverage": info.Leverage,
		"profit": info.Profit, "margin": info.Margin,
	})
}

func (e *Engine) handleMT5Positions(c *gin.Context) {
	if e.mt5Client == nil || !e.mt5Client.Connected() {
		c.JSON(503, gin.H{"error": "MT5 not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	positions, err := e.mt5Client.PositionsGet(ctx, nil)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	out := make([]gin.H, len(positions))
	for i, p := range positions {
		dir := "sell"
		if p.Type == gomt5.PositionTypeBuy {
			dir = "buy"
		}
		out[i] = gin.H{
			"ticket": p.Ticket, "symbol": p.Symbol, "type": dir,
			"volume": p.Volume, "price_open": p.PriceOpen, "price_current": p.PriceCurrent,
			"sl": p.PriceSL, "tp": p.PriceTP, "profit": p.Profit, "magic": p.Magic,
		}
	}
	c.JSON(200, gin.H{"positions": out})
}

// propFirmRealizedToday returns the prop-firm account's realized PnL for
// the current trading day (day boundary = 05:00 UTC, same as the bot's
// daily reset). Truth source: account BALANCE delta vs the day-start
// balance persisted in the ledger — immune to engine restarts, and NOT
// dependent on MT5 deal-history queries (the go-mt5 time-window deal
// query returns empty on this terminal; only per-ticket lookups work).
// The ledger seeds the day-start balance on the first check of each day.
func (e *Engine) propFirmRealizedToday() float64 {
	if e.mt5Client == nil || !e.mt5Client.Connected() {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	info, err := e.mt5Client.AccountInfo(ctx)
	cancel()
	if err != nil {
		log.Printf("propFirmRealizedToday: account info: %v", err)
		return 0
	}

	ledger := e.loadPropLedger()
	now := time.Now().UTC()
	dayKey := (now.Add(-5 * time.Hour)).UTC().Format("2006-01-02")
	start, ok := ledger[dayKey]
	if !ok {
		// First check of the day: record the current balance as the
		// day-start; nothing realized yet.
		ledger[dayKey] = info.Balance
		e.savePropLedger(ledger)
		log.Printf("PropFirm ledger: day %s starts at balance $%.2f", dayKey, info.Balance)
		return 0
	}
	return info.Balance - start
}

func (e *Engine) propLedgerPath() string {
	return filepath.Join("trading", "prop_ledger.json")
}

func (e *Engine) loadPropLedger() map[string]float64 {
	data, err := os.ReadFile(e.propLedgerPath())
	if err != nil {
		return map[string]float64{}
	}
	var out map[string]float64
	if json.Unmarshal(data, &out) != nil {
		return map[string]float64{}
	}
	if out == nil {
		out = map[string]float64{}
	}
	return out
}

func (e *Engine) savePropLedger(l map[string]float64) {
	data, _ := json.MarshalIndent(l, "", "  ")
	if err := os.MkdirAll(filepath.Dir(e.propLedgerPath()), 0o755); err != nil {
		log.Printf("PropFirm ledger: mkdir: %v", err)
		return
	}
	if err := os.WriteFile(e.propLedgerPath(), data, 0o644); err != nil {
		log.Printf("PropFirm ledger: write: %v", err)
	}
}

// propFirmEntryBlockReason returns a block reason when the prop-firm daily
// profit cap is reached; "" means new entries are allowed. Source of truth:
// realized PnL of deals closed since today 05:00 UTC on the broker — same
// boundary as the bot's daily reset — max'd with the bot's own tracked
// DailyPNL so restarts can't miss closed trades.
func (e *Engine) propFirmEntryBlockReason() string {
	if !e.propFirmAccount || e.propDailyProfitCap <= 0 {
		return ""
	}
	realized := e.propFirmRealizedToday()
	if botDaily := e.bot.GetDailyPNL(); botDaily > realized {
		realized = botDaily
	}
	if realized >= e.propDailyProfitCap {
		return fmt.Sprintf("daily profit cap reached: +$%.2f >= $%.2f - no new entries until tomorrow", realized, e.propDailyProfitCap)
	}
	return ""
}

// clampProfitTP tightens a position's take-profit so the realized profit
// can never exceed the daily profit cap (minus the $1.00 cost buffer).
// Applies only on the prop-firm account; personal accounts pass through.
func (e *Engine) clampProfitTP(direction string, price, tp, volume float64) float64 {
	if !e.propFirmAccount || e.propDailyProfitCap <= 0 {
		return tp
	}
	lockDist := (e.propDailyProfitCap - 1.0) / (volume * 100000)
	if direction == "BUY" {
		locked := price + lockDist
		if locked < tp {
			log.Printf("PROFIT CAP (propfirm): TP tightened from %.5f to %.5f (locks +$%.2f)", tp, locked, e.propDailyProfitCap-1.0)
			return locked
		}
	} else {
		locked := price - lockDist
		if locked > tp {
			log.Printf("PROFIT CAP (propfirm): TP tightened from %.5f to %.5f (locks +$%.2f)", tp, locked, e.propDailyProfitCap-1.0)
			return locked
		}
	}
	return tp
}

func (e *Engine) handleMT5Order(c *gin.Context) {
	var req struct {
		Symbol string  `json:"symbol" binding:"required"`
		Volume float64 `json:"volume" binding:"required,gt=0"`
		Type   string  `json:"type" binding:"required,oneof=buy sell"`
		SL     float64 `json:"sl"`
		TP     float64 `json:"tp"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	if e.mt5Client == nil || !e.mt5Client.Connected() {
		c.JSON(503, gin.H{"error": "MT5 not connected"})
		return
	}

	if reason := e.propFirmEntryBlockReason(); reason != "" {
		c.JSON(403, gin.H{"error": reason, "daily_profit_cap": e.propDailyProfitCap})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	tick, err := e.mt5Client.SymbolInfoTick(ctx, req.Symbol)
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("get price: %v", err)})
		return
	}

	var orderType gomt5.OrderType
	price := tick.Ask
	if req.Type == "sell" {
		orderType = gomt5.OrderTypeSell
		price = tick.Bid
	} else {
		orderType = gomt5.OrderTypeBuy
	}

	// Same daily-profit-cap TP tightening as the bot executor, so manual
	// (agent/AI) entries on the prop firm can never cross the cap either.
	req.TP = e.clampProfitTP(req.Type, price, req.TP, req.Volume)

	result, err := e.mt5Client.OrderSend(ctx, gomt5.TradeRequest{
		Action:      gomt5.TradeActionDeal,
		Symbol:      req.Symbol,
		Volume:      req.Volume,
		Type:        orderType,
		Price:       price,
		Deviation:   20,
		SL:          req.SL,
		TP:          req.TP,
		TypeFilling: gomt5.OrderFillingIOC,
	})
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"retcode": result.Retcode, "order": result.Order, "deal": result.Deal, "volume": result.Volume, "price": result.Price})
}

func (e *Engine) handleMT5Tick(c *gin.Context) {
	symbol := c.Param("symbol")
	if e.mt5Client == nil || !e.mt5Client.Connected() {
		c.JSON(503, gin.H{"error": "MT5 not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	if err := e.mt5Client.SymbolSelect(ctx, symbol, true); err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("select %s: %v", symbol, err)})
		return
	}
	tick, err := e.mt5Client.SymbolInfoTick(ctx, symbol)
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("tick %s: %v", symbol, err)})
		return
	}
	info, _ := e.mt5Client.SymbolInfo(ctx, symbol)
	digits := 5
	if info != nil {
		digits = int(info.Digits)
	}
	c.JSON(200, gin.H{
		"symbol": symbol, "bid": tick.Bid, "ask": tick.Ask, "last": tick.Last,
		"digits": digits, "time": tick.Time, "volume": tick.Volume,
	})
}

// handleMT5Select subscribes the symbol into the terminal's Market Watch
// so that subsequent SymbolInfoTick calls succeed. This is the real fix for
// MT5 retcode 10019 / "no tick available" — both can mean "symbol not yet
// in Market Watch on the connected terminal".
// handleMT5History returns closed deals for the last N hours. Same
// gomt5.HistoryDealsGet code path the bot's positionMonitor uses to
// realize PnL — exposed as an endpoint so the agent can answer "what
// closed today / did I breach the cap?" without waiting for a tracked
// ticket to disappear.
func (e *Engine) handleMT5History(c *gin.Context) {
	hoursStr := c.Param("hours")
	hours := 24
	if fmt.Sscanf(hoursStr, "%d", &hours); hours <= 0 || hours > 168 {
		hours = 24
	}
	if e.mt5Client == nil || !e.mt5Client.Connected() {
		c.JSON(503, gin.H{"error": "MT5 not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	to := time.Now()
	from := to.Add(-time.Duration(hours) * time.Hour)
	deals, err := e.mt5Client.HistoryDealsGet(ctx, &gomt5.HistoryFilter{
		DateFrom: from.Unix(),
		DateTo:   to.Unix(),
	})
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	out := make([]gin.H, 0, len(deals))
	totalProfit, totalSwap, totalCommission := 0.0, 0.0, 0.0
	wins, losses := 0, 0
	for _, d := range deals {
		// Only include closure deals (OUT / INOUT); entry deals have Profit=0.
		if d.Entry != gomt5.DealEntryOut && d.Entry != gomt5.DealEntryInOut {
			continue
		}
		out = append(out, gin.H{
			"ticket":      d.Ticket,
			"position_id": d.PositionID,
			"order":       d.Order,
			"time":         d.Time,
			"symbol":      d.Symbol,
			"type":        d.Type,
			"entry":       d.Entry.String(),
			"volume":      d.Volume,
			"price":       d.Price,
			"profit":      d.Profit,
			"swap":        d.Swap,
			"commission":  d.Commission,
			"comment":     d.Comment,
		})
		totalProfit += d.Profit
		totalSwap += d.Swap
		totalCommission += d.Commission
		if d.Profit > 0 {
			wins++
		} else if d.Profit < 0 {
			losses++
		}
	}
	c.JSON(200, gin.H{
		"hours":            hours,
		"from":             from.UTC().Format(time.RFC3339),
		"to":               to.UTC().Format(time.RFC3339),
		"closed_deals":     out,
		"count":            len(out),
		"total_profit":     totalProfit,
		"total_swap":       totalSwap,
		"total_commission": totalCommission,
		"net_pnl":          totalProfit + totalSwap + totalCommission,
		"wins":             wins,
		"losses":           losses,
	})
}

func (e *Engine) handleMT5Select(c *gin.Context) {
	symbol := c.Param("symbol")
	if e.mt5Client == nil || !e.mt5Client.Connected() {
		c.JSON(503, gin.H{"success": false, "error": "MT5 not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	if err := e.mt5Client.SymbolSelect(ctx, symbol, true); err != nil {
		c.JSON(500, gin.H{"success": false, "symbol": symbol, "error": fmt.Sprintf("SymbolSelect: %v", err)})
		return
	}
	// Probe with a tick fetch so the agent knows whether the symbol is now live.
	tick, err := e.mt5Client.SymbolInfoTick(ctx, symbol)
	if err != nil {
		c.JSON(200, gin.H{
			"success": true,
			"symbol":  symbol,
			"selected": true,
			"note":    "Symbol subscribed to Market Watch, but no tick yet — market may be closed or broker not streaming this symbol.",
		})
		return
	}
	c.JSON(200, gin.H{
		"success":  true,
		"symbol":  symbol,
		"selected": true,
		"bid":      tick.Bid,
		"ask":      tick.Ask,
		"time":     tick.Time,
	})
}

func (e *Engine) handleMT5Rates(c *gin.Context) {
	symbol := c.Param("symbol")
	tfStr := c.Param("timeframe")
	count := 100
	if c.Param("count") != "" {
		fmt.Sscanf(c.Param("count"), "%d", &count)
	}

	if e.mt5Client == nil || !e.mt5Client.Connected() {
		c.JSON(503, gin.H{"error": "MT5 not connected"})
		return
	}

	var tf gomt5.Timeframe
	switch tfStr {
	case "M1":
		tf = gomt5.TimeframeM1
	case "M5":
		tf = gomt5.TimeframeM5
	case "M15":
		tf = gomt5.TimeframeM15
	case "H1":
		tf = gomt5.TimeframeH1
	case "H4":
		tf = gomt5.TimeframeH4
	case "D1":
		tf = gomt5.TimeframeD1
	default:
		c.JSON(400, gin.H{"error": "invalid timeframe"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	rates, err := e.mt5Client.CopyRatesFromPos(ctx, symbol, tf, 0, count)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	out := make([]gin.H, len(rates))
	for i, r := range rates {
		out[i] = gin.H{"time": r.Time, "open": r.Open, "high": r.High, "low": r.Low, "close": r.Close, "volume": r.TickVolume}
	}
	c.JSON(200, out)
}

// requestLogger logs incoming HTTP requests
func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Printf("[%s] %s %s %d %v",
			c.Request.Method, c.Request.URL.Path,
			c.ClientIP(), c.Writer.Status(), time.Since(start))
	}
}



