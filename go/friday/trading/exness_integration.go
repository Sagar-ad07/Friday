package trading

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/friday-prototype/friday-go/trading"
	"github.com/gin-gonic/gin"
	gomt5 "github.com/mukbeast4/go-mt5"
)

func envFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f
		}
	}
	return fallback
}

// ──────────────────────────────────────────────────────────────────────
// P3: Exness personal-account MT5 integration.
//
// The Exness account (login 167036042 @ Exness-MT5Real3) is a SEPARATE
// MT5 terminal running from D:\MetaTrader 5\terminal64.exe. It's a
// personal account — no $150 daily loss cap, no 5% drawdown cap, no
// profit target. The user wants it to run 24/7 with Friday (the agent)
// placing trades on it on demand and honest PnL tracking.
//
// Architecture:
//   - Primary mt5Client stays on BlueGuardian (drives the autonomous TradingBot)
//   - mt5ClientExness is agent-managed (no bot loop, no cap)
//   - A lightweight position monitor goroutine tracks open tickets and
//     logs realized PnL via HistoryDealsGet — just honest recording, no
//     enforcement
//   - Tools (exness_account_info, execute_trade_exness, etc.) let the
//     agent query and trade this account independently
//
// Scalper spec (user directive, Jul 2026): the autonomous Exness bot
// trades TPCS entries with FIXED micro-lot risk — 0.01 lots, 10-pip SL
// (≈$1), 20-pip TP (≈$2), 1:2 R:R, 24/7, no cap. Tunable via
// EXNESS_LOT / EXNESS_SL_PIPS / EXNESS_TP_PIPS.
// ──────────────────────────────────────────────────────────────────────

// mt5PipeNameForPath computes the deterministic named-pipe path that the
// go-mt5 MT5 terminal server listens on. The algorithm mirrors the one in
// go-mt5's internal/pipe/conn_windows.go (SHA256 of UTF-16 encoded
// "\\?\" + lowercased terminal64.exe path).
func mt5PipeNameForPath(terminalPath string) string {
	input := `\\?\` + strings.ToLower(terminalPath)
	codes := utf16.Encode([]rune(input))
	buf := make([]byte, len(codes)*2)
	for i, c := range codes {
		buf[i*2] = byte(c)
		buf[i*2+1] = byte(c >> 8)
	}
	sum := sha256.Sum256(buf)
	return `\\.\pipe\MT5.Terminal.` + strings.ToUpper(hex.EncodeToString(sum[:]))
}

// connectExnessClient attempts to connect the second MT5 client to the
// Exness terminal via its computed pipe name. Called from Engine.Start()
// after the primary client is wired. After connection it spawns the
// autonomous Exness TradingBot loop (same TPCS strategy as BlueGuardian,
// no cap, low-balance notice at AED 10 per user directive).
func (e *Engine) connectExnessClient() {
	exnessPath := os.Getenv("EXNESS_TERMINAL_PATH")
	if exnessPath == "" {
		exnessPath = `D:\MetaTrader 5\terminal64.exe`
	}
	// Exness scalper risk config — fixed micro-lot spec per user:
	// 0.01 lots, $1 loss / $2 profit = 10-pip SL / 20-pip TP on EURUSDm.
	e.exnessLot = envFloat("EXNESS_LOT", 0.01)
	e.exnessSLPips = envFloat("EXNESS_SL_PIPS", 10)
	e.exnessTPPips = envFloat("EXNESS_TP_PIPS", 20)
	pipeName := mt5PipeNameForPath(exnessPath)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := gomt5.NewClient(ctx, gomt5.WithPipeName(pipeName), gomt5.WithTimeout(5*time.Second))
	if err != nil {
		log.Printf("Exness MT5 connect failed (pipe=%s): %v — Exness account will be unavailable", pipeName, err)
		return
	}

	e.mu.Lock()
	e.mt5ClientExness = client
	e.exnessConnected = true
	e.exnessTrackedTickets = make(map[int64]bool)
	e.mu.Unlock()

	info, err := client.AccountInfo(context.Background())
	if err != nil {
		log.Printf("Exness MT5 connected but AccountInfo failed: %v", err)
		return
	}
	log.Printf("Exness MT5 connected: login=%d server=%s balance=%.2f %s equity=%.2f",
		info.Login, info.Server, info.Balance, info.Currency, info.Equity)

	// Spawn the autonomous Exness bot — same TPCS strategy as BlueGuardian,
	// NO cap (personal account, 24/7 per user directive). The bot reads its
	// own initial balance from the live MT5 account, so sizing isn't tied
	// to a 5000-literal like the primary bot.
	e.exnessBot = trading.NewTradingBot(info.Balance, "trading/status_exness.json")
	// Exness streams EURUSDm, not plain EURUSD. The bot's symbol drives
	// the price feed fetch + the trade executor — so this is the ONE
	// account-specific override needed vs the BG bot.
	e.exnessBot.SetSymbol("EURUSDm")
	e.exnessBot.SetCapConfig(trading.CapConfig{Enabled: false, Limit: 0})
	e.exnessBot.SetSessionless(true)
	// Personal account — RecordTrade is just honest logging, no compliance.
	e.exnessBot.SetRecordTrade(func(pnl float64) (bool, string) {
		log.Printf("Exness realized pnl: %.2f %s (no cap, 24/7)", pnl, info.Currency)
		return true, ""
	})
	// Wire price feed — H1 candles, same as BG.
	e.exnessBot.SetPriceFeed(func(symbol string, count int) ([]trading.Candle, error) {
		if e.mt5ClientExness == nil || !e.mt5ClientExness.Connected() {
			return nil, fmt.Errorf("Exness MT5 not connected")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_ = e.mt5ClientExness.SymbolSelect(ctx, symbol, true)
		need := count
		if need < 300 {
			need = 300
		}
		rates, err := e.mt5ClientExness.CopyRatesFromPos(ctx, symbol, gomt5.TimeframeH1, 0, need)
		if err != nil || len(rates) == 0 {
			return nil, fmt.Errorf("Exness H1 rates: %v", err)
		}
		candles := make([]trading.Candle, len(rates))
		for i, r := range rates {
			candles[i] = trading.Candle{
				Time:   time.Unix(r.Time, 0).UTC(),
				Open:   r.Open, High: r.High, Low: r.Low, Close: r.Close,
				Volume: float64(r.TickVolume),
			}
		}
		return candles, nil
	})
	// Wire trade executor — TPCS entries with FIXED micro-lot risk per
	// user directive: 0.01 lots, 10-pip SL ($1), 20-pip TP ($2), 1:2 R:R.
	// No trade limit — bot trades whenever TPCS fires.
	// Bot places 0.01 lots (broker minimum) regardless of balance so the
	// account can actually trade at 64 AED. Per user: no min-balance stop,
	// just a notice below AED 10.
	e.exnessBot.SetTradeExecutor(func(symbol, direction string, volume, sl, tp float64) (bool, string) {
		if e.mt5ClientExness == nil || !e.mt5ClientExness.Connected() {
			return false, "Exness MT5 not connected"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := e.mt5ClientExness.SymbolSelect(ctx, symbol, true); err != nil {
			return false, fmt.Sprintf("select %s: %v", symbol, err)
		}
		tick, err := e.mt5ClientExness.SymbolInfoTick(ctx, symbol)
		if err != nil {
			return false, "tick: " + err.Error()
		}
		orderType := gomt5.OrderTypeBuy
		price := tick.Ask
		if direction == "SELL" {
			orderType = gomt5.OrderTypeSell
			price = tick.Bid
		}
	// Fixed micro-lot risk spec (user directive): 0.01 lots, $1 loss /
	// $2 profit = 10-pip SL / 20-pip TP on EURUSDm (pip ≈ $0.10 at 0.01
	// lots). TPCS still picks the DIRECTION and timing; risk is fixed.
	volume = e.exnessLot
	exnessPip := 0.0001
	slPips := e.exnessSLPips
	tpPips := e.exnessTPPips
	if direction == "BUY" {
		sl = price - slPips*exnessPip
		tp = price + tpPips*exnessPip
	} else {
		sl = price + slPips*exnessPip
		tp = price - tpPips*exnessPip
	}
		result, err := e.mt5ClientExness.OrderSend(ctx, gomt5.TradeRequest{
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
		// Register ticket for the Exness position monitor.
		e.exnessMu.Lock()
		if e.exnessTrackedTickets == nil {
			e.exnessTrackedTickets = make(map[int64]bool)
		}
		e.exnessTrackedTickets[result.Order] = true
		e.exnessMu.Unlock()
		// Check low-balance notice threshold (user directive: notice at AED 10).
		if acc, err := e.mt5ClientExness.AccountInfo(ctx); err == nil && acc.Balance < 10.0 {
			e.mu.Lock()
			e.exnessLowBalance = true
			e.mu.Unlock()
			log.Printf("⚠️ Exness LOW BALANCE NOTICE: balance=%.2f %s (below AED 10) — bot keeps trading but Friday should flag this", acc.Balance, acc.Currency)
		}
		return true, fmt.Sprintf("order=%d deal=%d price=%.5f", result.Order, result.Deal, result.Price)
	})
	// Wire position monitor (already exists — adopt-orphan + realize PnL,
	// no cap enforcement).
	e.exnessBot.SetPositionMonitor(
		func() ([]trading.OpenPosition, error) {
			if e.mt5ClientExness == nil || !e.mt5ClientExness.Connected() {
				return nil, fmt.Errorf("Exness MT5 not connected")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			positions, err := e.mt5ClientExness.PositionsGet(ctx, nil)
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
					Ticket: p.Ticket, Symbol: p.Symbol, Type: dir,
					Volume: p.Volume, PriceOpen: p.PriceOpen, PriceCurrent: p.PriceCurrent,
					Profit: p.Profit, SL: p.PriceSL, TP: p.PriceTP,
				})
			}
			return out, nil
		},
		func(ticket int64) (float64, bool, error) {
			if e.mt5ClientExness == nil || !e.mt5ClientExness.Connected() {
				return 0, false, fmt.Errorf("Exness MT5 not connected")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			deals, err := e.mt5ClientExness.HistoryDealsGet(ctx, &gomt5.HistoryFilter{Ticket: ticket})
			if err != nil {
				return 0, false, err
			}
			var totalPnl float64
			found := false
			for _, d := range deals {
				if d.Entry == gomt5.DealEntryOut || d.Entry == gomt5.DealEntryInOut {
					totalPnl += d.Profit + d.Swap + d.Commission
					found = true
				}
			}
			return totalPnl, found, nil
		},
	)
	// Start the autonomous Exness bot — 24/7, runs T PCS same as BG.
	go e.exnessBot.Start()
	log.Printf("Exness autonomous bot started (TPCS, no cap, 24/7)")

	// Stop any previous monitor goroutine before starting a new one.
	// Without this, every reconnect spawns an orphaned goroutine that
	// holds stale references and wastes resources forever.
	e.mu.Lock()
	if e.exnessStopCh != nil {
		close(e.exnessStopCh)
		e.exnessMonWg.Wait()
	}
	e.exnessStopCh = make(chan struct{})
	e.mu.Unlock()

	// Start the Exness position monitor (no cap, just honest PnL logging).
	// This runs in parallel with the bot's own positionMonitor — the
	// bot's monitor realizes PnL into BotState, this one just additionally
	// adopts orphan tickets placed via execute_trade_exness and logs them.
	e.exnessMonWg.Add(1)
	go e.monitorExnessPositions()
}

// monitorExnessPositions polls open positions on the Exness account and
// logs realized PnL when tickets close. Unlike the BlueGuardian bot's
// positionMonitor, this one has NO cap enforcement — personal account,
// 24/7, no profit limit per user directive.
func (e *Engine) monitorExnessPositions() {
	defer e.exnessMonWg.Done()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("monitorExnessPositions panicked: %v — restarting in 30s", r)
			time.Sleep(30 * time.Second)
			e.mu.RLock()
			ch := e.exnessStopCh
			e.mu.RUnlock()
			if ch != nil {
				e.exnessMonWg.Add(1)
				go e.monitorExnessPositions()
			}
		}
	}()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	e.mu.RLock()
	stopCh := e.exnessStopCh
	e.mu.RUnlock()

	for {
		select {
		case <-stopCh:
			log.Println("Exness position monitor stopped (reconnect/new session)")
			return
		case <-ticker.C:
			e.mu.RLock()
			client := e.mt5ClientExness
			connected := e.exnessConnected
			tracked := e.exnessTrackedTickets
			e.mu.RUnlock()
			if !connected || client == nil || !client.Connected() {
				continue
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			positions, err := client.PositionsGet(ctx, nil)
			cancel()
			if err != nil {
				continue
			}

			liveTickets := make(map[int64]bool, len(positions))
			for _, p := range positions {
				liveTickets[p.Ticket] = true
			}

			// Adopt orphans (tickets the agent placed via execute_trade_exness
			// that we haven't registered yet).
			e.exnessMu.Lock()
			for _, p := range positions {
				if !tracked[p.Ticket] {
					tracked[p.Ticket] = true
					log.Printf("📋 Exness: adopted orphan position ticket=%d (%s %s %.2f lots)",
						p.Ticket, p.Symbol, positionTypeString(p.Type), p.Volume)
				}
			}
			// Detect closures
			var closedThisTick []int64
			for t := range tracked {
				if !liveTickets[t] {
					closedThisTick = append(closedThisTick, t)
				}
			}
			e.exnessMu.Unlock()

			if len(closedThisTick) == 0 {
				continue
			}

			// Realize PnL for each closed ticket — honest recording, no cap.
			for _, tkt := range closedThisTick {
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				deals, err := client.HistoryDealsGet(ctx, &gomt5.HistoryFilter{Ticket: tkt})
				cancel()
				if err != nil {
					continue
				}
				var totalPnl float64
				found := false
				for _, d := range deals {
					if d.Entry == gomt5.DealEntryOut || d.Entry == gomt5.DealEntryInOut {
						totalPnl += d.Profit + d.Swap + d.Commission
						found = true
					}
				}
				if found {
					e.exnessMu.Lock()
					delete(tracked, tkt)
					e.exnessMu.Unlock()
					log.Printf("💰 Exness POSITION CLOSED: ticket=%d pnl=%.2f (no cap — personal account 24/7)", tkt, totalPnl)
				}
			}
		}
	}
}

func positionTypeString(t gomt5.PositionType) string {
	if t == gomt5.PositionTypeBuy {
		return "buy"
	}
	return "sell"
}

// ──────────────────────────────────────────────────────────────────────
// Exness HTTP endpoints — mirror the primary /mt5/* endpoints but route
// to the second MT5 client. All return honest data from the live broker.
// ──────────────────────────────────────────────────────────────────────

func (e *Engine) setupExnessRoutes() {
	e.router.GET("/mt5/exness/account", e.handleExnessAccount)
	e.router.GET("/mt5/exness/positions", e.handleExnessPositions)
	e.router.POST("/mt5/exness/order", e.handleExnessOrder)
	e.router.GET("/mt5/exness/history/:hours", e.handleExnessHistory)
	e.router.POST("/mt5/exness/select/:symbol", e.handleExnessSelect)
	e.router.GET("/mt5/exness/tick/:symbol", e.handleExnessTick)
	// Exness autonomous bot status (parallel to /trading/status on BG).
	// Mirrors what Friday would see about the BG bot — running, in_trade,
	// ticket, last_signal, wins, losses, daily_pnl etc.
	e.router.GET("/trading/exness/status", e.handleExnessBotStatus)
	e.router.GET("/trading/exness/market-analysis", e.handleExnessMarketAnalysis)
}

// handleExnessBotStatus surfaces the autonomous Exness TradingBot's
// state so Friday can monitor it the same way she monitors the BG bot.
func (e *Engine) handleExnessBotStatus(c *gin.Context) {
	if e.exnessBot == nil {
		c.JSON(200, gin.H{
			"running":          false,
			"connected":        e.exnessConnected,
			"low_balance_notice": e.exnessLowBalance,
			"note":             "Exness autonomous bot not initialized — check engine logs for connection failure",
		})
		return
	}
	status := e.exnessBot.GetStatus()
	status["account"] = "exness"
	status["low_balance_notice"] = e.exnessLowBalance
	status["strategy"] = "TPCS entries, fixed scalper risk"
	status["lot"] = e.exnessLot
	status["sl_pips"] = e.exnessSLPips
	status["tp_pips"] = e.exnessTPPips
	status["risk_usd"] = e.exnessSLPips * 0.10 * (e.exnessLot / 0.01)
	status["reward_usd"] = e.exnessTPPips * 0.10 * (e.exnessLot / 0.01)
	if e.exnessLowBalance {
		status["low_balance_message"] = "Exness balance below AED 10 — bot keeps trading per user directive but Friday should flag this to Boss"
	}
	c.JSON(200, status)
}

func (e *Engine) handleExnessMarketAnalysis(c *gin.Context) {
	if !e.exnessConnected || e.mt5ClientExness == nil {
		c.JSON(503, gin.H{"error": "Exness MT5 not connected"})
		return
	}
	symbol := c.DefaultQuery("symbol", "EURUSDm")
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	_ = e.mt5ClientExness.SymbolSelect(ctx, symbol, true)
	rates, err := e.mt5ClientExness.CopyRatesFromPos(ctx, symbol, gomt5.TimeframeH1, 0, 100)
	if err != nil || len(rates) < 28 {
		c.JSON(503, gin.H{"error": "not enough candles", "got": len(rates), "err": fmt.Sprintf("%v", err)})
		return
	}
	ie := trading.NewIntelligenceEngine()
	for _, r := range rates {
		ie.RecordPrice(r.Close)
		ie.RecordVolume(float64(r.TickVolume))
	}
	market := ie.AnalyzeMarket()
	if market == nil {
		c.JSON(503, gin.H{"error": "market analysis returned nil"})
		return
	}
	c.JSON(200, gin.H{
		"regime":         market.Regime.String(),
		"confidence":     market.Confidence,
		"adx":            market.ADX,
		"atr":            market.ATR,
		"rsi":            market.RSI,
		"volatility":     market.Volatility,
		"trend_strength": market.TrendStrength,
		"support":        market.Support,
		"resistance":     market.Resistance,
		"samples":        len(rates),
		"symbol":         symbol,
	})
}

func (e *Engine) handleExnessAccount(c *gin.Context) {
	if e.mt5ClientExness == nil || !e.mt5ClientExness.Connected() {
		c.JSON(503, gin.H{"connected": false, "error": "Exness MT5 not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	info, err := e.mt5ClientExness.AccountInfo(ctx)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{
		"login": info.Login, "server": info.Server, "balance": info.Balance,
		"equity": info.Equity, "currency": info.Currency, "leverage": info.Leverage,
		"profit": info.Profit, "margin": info.Margin,
	})
}

func (e *Engine) handleExnessPositions(c *gin.Context) {
	if e.mt5ClientExness == nil || !e.mt5ClientExness.Connected() {
		c.JSON(503, gin.H{"connected": false, "error": "Exness MT5 not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	positions, err := e.mt5ClientExness.PositionsGet(ctx, nil)
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

func (e *Engine) handleExnessOrder(c *gin.Context) {
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
	if e.mt5ClientExness == nil || !e.mt5ClientExness.Connected() {
		c.JSON(503, gin.H{"connected": false, "error": "Exness MT5 not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	if err := e.mt5ClientExness.SymbolSelect(ctx, req.Symbol, true); err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("select %s: %v", req.Symbol, err)})
		return
	}
	tick, err := e.mt5ClientExness.SymbolInfoTick(ctx, req.Symbol)
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("tick %s: %v", req.Symbol, err)})
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
	result, err := e.mt5ClientExness.OrderSend(ctx, gomt5.TradeRequest{
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
	// Register the ticket for position monitoring
	e.exnessMu.Lock()
	if e.exnessTrackedTickets == nil {
		e.exnessTrackedTickets = make(map[int64]bool)
	}
	e.exnessTrackedTickets[result.Order] = true
	e.exnessMu.Unlock()
	c.JSON(200, gin.H{
		"retcode": result.Retcode, "order": result.Order, "deal": result.Deal,
		"volume": result.Volume, "price": result.Price,
		"account": "exness", "note": "no cap — personal account 24/7",
	})
}

func (e *Engine) handleExnessHistory(c *gin.Context) {
	hoursStr := c.Param("hours")
	hours := 24
	if fmt.Sscanf(hoursStr, "%d", &hours); hours <= 0 || hours > 168 {
		hours = 24
	}
	if e.mt5ClientExness == nil || !e.mt5ClientExness.Connected() {
		c.JSON(503, gin.H{"connected": false, "error": "Exness MT5 not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	to := time.Now()
	from := to.Add(-time.Duration(hours) * time.Hour)
	deals, err := e.mt5ClientExness.HistoryDealsGet(ctx, &gomt5.HistoryFilter{
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
		if d.Entry != gomt5.DealEntryOut && d.Entry != gomt5.DealEntryInOut {
			continue
		}
		out = append(out, gin.H{
			"ticket": d.Ticket, "position_id": d.PositionID,
			"order": d.Order, "time": d.Time, "symbol": d.Symbol,
			"type": d.Type, "entry": d.Entry.String(),
			"volume": d.Volume, "price": d.Price,
			"profit": d.Profit, "swap": d.Swap, "commission": d.Commission,
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
		"account":          "exness",
		"note":             "no cap — personal account, 24/7 trading per user directive",
	})
}

func (e *Engine) handleExnessSelect(c *gin.Context) {
	symbol := c.Param("symbol")
	if e.mt5ClientExness == nil || !e.mt5ClientExness.Connected() {
		c.JSON(503, gin.H{"connected": false, "error": "Exness MT5 not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	if err := e.mt5ClientExness.SymbolSelect(ctx, symbol, true); err != nil {
		c.JSON(500, gin.H{"success": false, "symbol": symbol, "error": err.Error()})
		return
	}
	tick, err := e.mt5ClientExness.SymbolInfoTick(ctx, symbol)
	if err != nil {
		c.JSON(200, gin.H{"success": true, "symbol": symbol, "selected": true, "note": "subscribed but no tick yet"})
		return
	}
	c.JSON(200, gin.H{"success": true, "symbol": symbol, "selected": true, "bid": tick.Bid, "ask": tick.Ask, "time": tick.Time})
}

func (e *Engine) handleExnessTick(c *gin.Context) {
	symbol := c.Param("symbol")
	if e.mt5ClientExness == nil || !e.mt5ClientExness.Connected() {
		c.JSON(503, gin.H{"connected": false, "error": "Exness MT5 not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	if err := e.mt5ClientExness.SymbolSelect(ctx, symbol, true); err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("select %s: %v", symbol, err)})
		return
	}
	tick, err := e.mt5ClientExness.SymbolInfoTick(ctx, symbol)
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("tick %s: %v", symbol, err)})
		return
	}
	info, _ := e.mt5ClientExness.SymbolInfo(ctx, symbol)
	digits := 5
	if info != nil {
		digits = int(info.Digits)
	}
	c.JSON(200, gin.H{"symbol": symbol, "bid": tick.Bid, "ask": tick.Ask, "last": tick.Last, "digits": digits, "time": tick.Time, "volume": tick.Volume})
}