package trading

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/friday-prototype/friday-go/config"
	"github.com/friday-prototype/friday-go/internal/infrastructure/execution"
	"github.com/friday-prototype/friday-go/pipeline"
	"github.com/friday-prototype/friday-go/safety"
)

type BotState struct {
	Running          bool
	InTrade          bool
	Ticket           int64
	DailyPNL         float64
	TotalPNL         float64
	PeakBalance      float64
	InitialBalance   float64
	ProfitableDays   int
	TradesToday      int
	HighestDayPNL    float64
	GuardianStrikes  int
	LastTradeResult  string
	LastError        string
	LastSignal       string
	LastRegime       string
	LastFusionConf   float64
	LastADX          float64
	LastATR          float64
	ActiveStrategy   string
	StratWins        int
	StratLosses      int
	StratTotalPnL    float64
	DayCounted       bool
	Wins             int
	Losses           int
	mu               sync.RWMutex
}

type OpenPosition struct {
	Ticket        int64
	Symbol        string
	Type          string
	Volume        float64
	PriceOpen     float64
	PriceCurrent  float64
	Profit        float64
	SL            float64
	TP            float64
}

type PriceFeedFn func(symbol string, count int) ([]Candle, error)
type TradeExecFn func(symbol, direction string, volume, sl, tp float64) (bool, string)
type OpenPositionsFn func() ([]OpenPosition, error)
type ClosedDealFn func(ticket int64) (pnl float64, found bool, err error)
type RecordTradeFn func(pnl float64) (allowed bool, violation string)

type LedgerFn func(ticket int64, symbol, direction string, lots, openPrice, closePrice float64, pnl float64)

// StrategyUpdaterFn applies new strategy parameters from the Strategy Lab.
// Called after Boss approves a new strategy. Only applied when no position is open.
type StrategyUpdaterFn func(emaFast, emaSlow, emaPullback, rsiPeriod, atrPeriod int, slMult, tpMult float64)

type CapConfig struct {
	Enabled     bool
	Limit       float64 // Daily LOSS limit ($). Bot halts when realized daily PnL <= -Limit.
	ProfitLimit float64 // Daily PROFIT cap ($). 0 = disabled. Bot halts when realized daily PnL >= ProfitLimit.
}


type TradingBot struct {
	state          *BotState
	execution      *execution.ExecutionEngine
	feed           *pipeline.UltraLowLatencyFeed
	guardians      *safety.TradingGuardians
	config         *config.Config
	stateFile      string
	symbol         string
	intel          *IntelligenceEngine
	strategy       *RegimeBasedStrategy
	fusion         *SignalFusion
	fetchCandles   PriceFeedFn
	executeTrade   TradeExecFn
	openPositions  OpenPositionsFn
	closedDeal     ClosedDealFn
	recordTrade    RecordTradeFn
	ledgerFn       LedgerFn
	strategyUpdater StrategyUpdaterFn
	capConfig      CapConfig
	sessionless    bool
	trackedTickets map[int64]bool
	mu             sync.RWMutex
}

func (b *TradingBot) SetSessionless(val bool) { b.sessionless = val }

func NewTradingBot(initialBalance float64, stateFile string) *TradingBot {
	if stateFile == "" {
		stateFile = "trading/status.json"
	}
	b := &TradingBot{
		state: &BotState{
			InitialBalance: initialBalance,
			PeakBalance:    initialBalance,
		},
		execution:  execution.NewExecutionEngine(),
		feed:       pipeline.NewUltraLowLatencyFeed(),
		guardians:  safety.NewTradingGuardians(initialBalance),
		config:     config.GetConfig(),
		stateFile:  stateFile,
		symbol:     "EURUSD",
		intel:      NewIntelligenceEngine(),
		strategy:   NewRegimeBasedStrategy(),
		trackedTickets: make(map[int64]bool),
	}
	b.loadStatus()
	return b
}

func (b *TradingBot) SetPriceFeed(fn PriceFeedFn) { b.fetchCandles = fn }

func (b *TradingBot) SetSymbol(symbol string) {
	b.mu.Lock()
	b.symbol = symbol
	b.mu.Unlock()
}

func (b *TradingBot) SetTradeExecutor(fn TradeExecFn) { b.executeTrade = fn }

func (b *TradingBot) SetPositionMonitor(opens OpenPositionsFn, closed ClosedDealFn) {
	b.mu.Lock()
	b.openPositions = opens
	b.closedDeal = closed
	b.mu.Unlock()
}

func (b *TradingBot) SetRecordTrade(fn RecordTradeFn) {
	b.mu.Lock()
	b.recordTrade = fn
	b.mu.Unlock()
}

func (b *TradingBot) SetLedger(fn LedgerFn) {
	b.mu.Lock()
	b.ledgerFn = fn
	b.mu.Unlock()
}

func (b *TradingBot) SetStrategyUpdater(fn StrategyUpdaterFn) {
	b.mu.Lock()
	b.strategyUpdater = fn
	b.mu.Unlock()
}

func (b *TradingBot) SetCapConfig(cfg CapConfig) {
	b.mu.Lock()
	b.capConfig = cfg
	b.mu.Unlock()
	log.Printf("Bot cap config: enabled=%v limit=$%.2f profit_limit=$%.2f", cfg.Enabled, cfg.Limit, cfg.ProfitLimit)
}

// GetDailyPNL returns the realized PnL for the current trading day.
func (b *TradingBot) GetDailyPNL() float64 {
	b.state.mu.RLock()
	defer b.state.mu.RUnlock()
	return b.state.DailyPNL
}

// SetDailyPNL forces the tracked daily PnL (used to re-sync with broker
// truth after a restart, so a restart can't reset the day's realized PnL
// and re-enable trading past the caps).
func (b *TradingBot) SetDailyPNL(v float64) {
	b.state.mu.Lock()
	defer b.state.mu.Unlock()
	if v > b.state.DailyPNL {
		b.state.DailyPNL = v
	}
}

func (b *TradingBot) Fusion() *SignalFusion { return b.fusion }

func (b *TradingBot) Start() error {
	b.state.mu.Lock()
	b.state.Running = true
	b.state.mu.Unlock()

	b.writeStatus()

	go b.run()
	go b.positionMonitor()
	return nil
}

func (b *TradingBot) positionMonitor() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("positionMonitor panicked: %v - restarting in 30s", r)
			time.Sleep(30 * time.Second)
			go b.positionMonitor()
		}
	}()
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for range tick.C {
		b.state.mu.RLock()
		running := b.state.Running
		b.state.mu.RUnlock()
		if !running {
			return
		}

		b.mu.Lock()
		opens := b.openPositions
		closed := b.closedDeal
		recordTrade := b.recordTrade
		ledger := b.ledgerFn
		tracked := b.trackedTickets
		b.mu.Unlock()
		if opens == nil {
			continue
		}

		live, err := opens()
		if err != nil {
			continue
		}

		liveTickets := make(map[int64]bool, len(live))
		for _, p := range live {
			liveTickets[p.Ticket] = true
		}

		for _, p := range live {
			if !tracked[p.Ticket] {
				tracked[p.Ticket] = true
				log.Printf("Adopted orphan position ticket=%d (%s %.2f lots) for PnL tracking", p.Ticket, p.Type, p.Volume)
			}
		}

		var closedThisTick []int64
		for t := range tracked {
			if !liveTickets[t] {
				closedThisTick = append(closedThisTick, t)
			}
		}

		if len(closedThisTick) == 0 {
			b.syncInTrade(live)
			continue
		}

		posMap := make(map[int64]OpenPosition, len(live))
		for _, p := range live {
			posMap[p.Ticket] = p
		}

		var stopForViolation string
		b.state.mu.Lock()
		for _, tkt := range closedThisTick {
			delete(tracked, tkt)
			if closed == nil {
				continue
			}
			pnl, found, err := closed(tkt)
			if err != nil || !found {
				continue
			}
			b.state.DailyPNL += pnl
			b.state.TotalPNL += pnl
			if pnl > 0 {
				b.state.Wins++
				b.state.LastTradeResult = fmt.Sprintf("win $%.2f ticket=%d", pnl, tkt)
			} else if pnl < 0 {
				b.state.Losses++
				b.state.LastTradeResult = fmt.Sprintf("loss $%.2f ticket=%d", pnl, tkt)
			}
			if pnl > b.state.HighestDayPNL {
				b.state.HighestDayPNL = pnl
			}
			currentEquity := b.state.InitialBalance + b.state.TotalPNL
			if currentEquity > b.state.PeakBalance {
				b.state.PeakBalance = currentEquity
			}
			log.Printf("POSITION CLOSED: ticket=%d pnl=$%.2f | daily=$%.2f total=$%.2f wins=%d losses=%d", tkt, pnl, b.state.DailyPNL, b.state.TotalPNL, b.state.Wins, b.state.Losses)

			if recordTrade != nil {
				allowed, violation := recordTrade(pnl)
				if !allowed && violation != "" {
					stopForViolation = violation
				}
			}
			if ledger != nil {
				if pos, ok := posMap[tkt]; ok {
					ledger(tkt, pos.Symbol, pos.Type, pos.Volume, pos.PriceOpen, pos.PriceCurrent, pnl)
				}
			}
		}
		b.state.mu.Unlock()

		b.syncInTrade(live)
		b.writeStatus()

		if stopForViolation != "" {
			log.Printf("PROP FIRM VIOLATION: %s - stopping bot permanently until reset", stopForViolation)
			b.state.mu.Lock()
			b.state.LastError = fmt.Sprintf("PROP FIRM VIOLATION: %s", stopForViolation)
			b.state.Running = false
			b.state.mu.Unlock()
			b.writeStatus()
			return
		}

		b.mu.RLock()
		cap := b.capConfig
		b.mu.RUnlock()
		if cap.Enabled && cap.Limit > 0 {
			b.state.mu.RLock()
			daily := b.state.DailyPNL
			b.state.mu.RUnlock()
			if daily <= -cap.Limit {
				log.Printf("DAILY LOSS LIMIT HIT (bot-side): daily_pnl=$%.2f - stopping bot", daily)
				b.state.mu.Lock()
				b.state.LastError = fmt.Sprintf("DAILY LOSS LIMIT: $%.2f - bot stopped after realized losses", daily)
				b.state.Running = false
				b.state.mu.Unlock()
				b.writeStatus()
				return
			}
		}
		if cap.ProfitLimit > 0 {
			b.state.mu.RLock()
			daily := b.state.DailyPNL
			b.state.mu.RUnlock()
			if daily >= cap.ProfitLimit {
				log.Printf("DAILY PROFIT CAP HIT (bot-side): daily_pnl=$%.2f cap=$%.2f - stopping bot for today", daily, cap.ProfitLimit)
				b.state.mu.Lock()
				b.state.LastError = fmt.Sprintf("DAILY PROFIT CAP: +$%.2f (cap $%.2f) - no new trades until tomorrow", daily, cap.ProfitLimit)
				b.state.Running = false
				b.state.mu.Unlock()
				b.writeStatus()
				return
			}
		}
	}
}

func (b *TradingBot) syncInTrade(live []OpenPosition) {
	b.state.mu.Lock()
	defer b.state.mu.Unlock()
	if len(live) > 0 {
		b.state.InTrade = true
		b.state.Ticket = live[0].Ticket
	} else {
		b.state.InTrade = false
		b.state.Ticket = 0
	}
}

func (b *TradingBot) Stop() {
	b.state.mu.Lock()
	b.state.Running = false
	b.state.mu.Unlock()
	b.writeStatus()
}

func (b *TradingBot) run() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Bot panicked: %v - restarting in 10s", r)
			time.Sleep(10 * time.Second)
			b.state.mu.Lock()
			b.state.Running = true
			b.state.InTrade = false
			b.state.mu.Unlock()
			b.writeStatus()
			go b.run()
		}
	}()
	for {
		b.state.mu.RLock()
		running := b.state.Running
		b.state.mu.RUnlock()
		if !running {
			break
		}
		now := time.Now().UTC()
		b.handleDailyReset(now)
		if breach := b.checkRules(); breach != "" {
			log.Printf("Rule breach: %s", breach)
			b.state.mu.Lock()
			b.state.LastError = breach
			b.state.Running = false
			b.state.mu.Unlock()
			b.writeStatus()
			break
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("IntelCycle panicked: %v", r)
				}
			}()
			// Only trade during session hours (lab-controlled or TPCS default 08-20 UTC)
			if !b.sessionless && !b.inSession() {
				return
			}
			// DISABLED: Skip during high-impact news (NFP, CPI, FOMC, etc.)
			// if IsNewsTime(now) {
			// 	log.Printf("Bot: %s - suppressing trade during news window", b.symbol)
			// 	return
			// }
			fusion := b.intelligentCycle()
			if fusion != nil && fusion.Confidence >= 0.50 && !b.state.InTrade {
				b.executeFusionTrade(fusion)
				time.Sleep(60 * time.Second)
			}
		}()
		time.Sleep(5 * time.Second)
		b.writeStatus()
	}
}

func (b *TradingBot) intelligentCycle() *SignalFusion {
	if b.fetchCandles == nil {
		return nil
	}
	candles, err := b.fetchCandles(b.symbol, 120)
	if err != nil || len(candles) < 75 {
		return nil
	}
	for _, c := range candles {
		b.intel.RecordPrice(c.Close)
		b.intel.RecordVolume(c.Volume)
	}
	market := b.intel.AnalyzeMarket()
	if market == nil {
		return nil
	}
	b.strategy.SetMarket(market)
	allStrats := b.strategy.AllStrategies()
	activeName := b.strategy.ActiveName()
	fusion := b.intel.FusionDecision(allStrats, market)
	b.state.mu.Lock()
	if market != nil {
		b.state.LastRegime = market.Regime.String()
		b.state.LastADX = market.ADX
		b.state.LastATR = market.ATR
	}
	if fusion != nil {
		b.fusion = fusion
		b.state.LastSignal = fusion.Direction
		b.state.LastFusionConf = fusion.Confidence
		b.state.ActiveStrategy = activeName
	} else {
		b.state.LastSignal = "none"
		b.state.LastFusionConf = 0
		b.state.ActiveStrategy = activeName
	}
	b.state.mu.Unlock()
	return fusion
}

func (b *TradingBot) executeFusionTrade(fusion *SignalFusion) {
	b.state.mu.RLock()
	bal := b.state.InitialBalance
	wins := b.state.StratWins
	losses := b.state.StratLosses
	b.state.mu.RUnlock()

	// $18 risk / $36 profit per trade (0.15 lots, 1:2 R:R)
	// 12 pip SL � $1.50/pip = $18 | 24 pip TP � $1.50/pip = $36
	var volume float64
	switch {
	case bal >= 5000:
		volume = 0.15
	case bal >= 1000:
		volume = 0.10
	case bal >= 100:
		volume = 0.02
	default:
		volume = 0.01
	}

	// Reduce during losing streaks
	totalTrades := wins + losses
	if totalTrades >= 3 && losses > wins {
		volume *= 0.5
	}
	if volume < 0.01 {
		volume = 0.01
	}

	sl := fusion.StopLoss
	tp := fusion.TakeProfit
	if b.executeTrade != nil {
		success, msg := b.executeTrade(b.symbol, fusion.Direction, volume, sl, tp)
		b.state.mu.Lock()
		if success {
			b.state.InTrade = true
			b.state.TradesToday++
			b.state.LastTradeResult = fusion.Direction + "_LIVE"
			if tkt, ok := parseTicketFromMsg(msg); ok {
				b.mu.Lock()
				if b.trackedTickets == nil {
					b.trackedTickets = make(map[int64]bool)
				}
				b.trackedTickets[tkt] = true
				b.mu.Unlock()
				b.state.Ticket = tkt
			}
		} else {
			b.state.InTrade = false
			b.state.LastError = "exec: " + msg
		}
		b.state.mu.Unlock()
	} else {
		// LIVE ONLY: no paper/simulated trades. If no live executor is
		// wired, the signal is dropped — never fabricate PnL.
		log.Printf("No live trade executor for %s %s — signal dropped (no paper trading)", b.symbol, fusion.Direction)
		b.state.mu.Lock()
		b.state.InTrade = false
		b.state.LastError = "no live executor - paper trading disabled"
		b.state.mu.Unlock()
	}
}

func (b *TradingBot) handleDailyReset(now time.Time) {
	b.state.mu.Lock()
	defer b.state.mu.Unlock()
	if now.Hour() == 5 && now.Minute() < 1 && !b.state.DayCounted {
		b.state.DailyPNL = 0
		b.state.TradesToday = 0
		b.state.DayCounted = true
		b.state.LastTradeResult = ""
		b.writeStatus()
	}
	if now.Hour() != 5 {
		b.state.DayCounted = false
	}
}

func (b *TradingBot) checkRules() string {
	b.state.mu.RLock()
	daily := b.state.DailyPNL
	total := b.state.TotalPNL
	peak := b.state.PeakBalance
	currentBalance := peak + total
	b.state.mu.RUnlock()
	b.mu.RLock()
	cap := b.capConfig
	b.mu.RUnlock()
	if cap.Enabled && cap.Limit > 0 && daily <= -cap.Limit {
		return fmt.Sprintf("Daily loss limit hit: $%.2f (cap $%.2f)", daily, cap.Limit)
	}
	if cap.Enabled && cap.ProfitLimit > 0 && daily >= cap.ProfitLimit {
		return fmt.Sprintf("Daily profit cap hit: +$%.2f (cap $%.2f) - no new trades until tomorrow", daily, cap.ProfitLimit)
	}
	if cap.Enabled && peak > 0 {
		trailingStop := peak * 0.80
		if currentBalance <= trailingStop {
			return fmt.Sprintf("Trailing drawdown breached: equity $%.2f <= trail $%.2f", currentBalance, trailingStop)
		}
	}
	return ""
}

func (b *TradingBot) writeStatus() {
	b.mu.Lock()
	tracked := make([]int64, 0, len(b.trackedTickets))
	for t := range b.trackedTickets {
		tracked = append(tracked, t)
	}
	b.mu.Unlock()
	data := map[string]interface{}{
		"running":          b.state.Running,
		"in_trade":         b.state.InTrade,
		"ticket":           b.state.Ticket,
		"daily_pnl":        b.state.DailyPNL,
		"total_pnl":        b.state.TotalPNL,
		"peak_balance":     b.state.PeakBalance,
		"profitable_days":  b.state.ProfitableDays,
		"trades_today":     b.state.TradesToday,
		"highest_day_pnl":  b.state.HighestDayPNL,
		"guardian_strikes": b.state.GuardianStrikes,
		"last_trade_result": b.state.LastTradeResult,
		"last_error":       b.state.LastError,
		"daily_profit_cap": b.capConfig.ProfitLimit,
		"wins":             b.state.Wins,
		"losses":           b.state.Losses,
		"tracked_tickets":  tracked,
		"timestamp":        time.Now().UTC().Format(time.RFC3339),
	}
	jsonData, _ := json.MarshalIndent(data, "", "  ")
	if err := os.MkdirAll(filepath.Dir(b.stateFile), 0755); err != nil {
		log.Printf("Error creating status dir: %v", err)
		return
	}
	os.WriteFile(b.stateFile, jsonData, 0644)
}

func (b *TradingBot) loadStatus() {
	data, err := os.ReadFile(b.stateFile)
	if err != nil {
		return
	}
	var raw map[string]interface{}
	if json.Unmarshal(data, &raw) != nil {
		return
	}
	toFloat := func(v interface{}) float64 {
		if f, ok := v.(float64); ok { return f }
		return 0
	}
	toInt := func(v interface{}) int {
		if f, ok := v.(float64); ok { return int(f) }
		return 0
	}
	toString := func(v interface{}) string {
		if s, ok := v.(string); ok { return s }
		return ""
	}
	toTickets := func(v interface{}) []int64 {
		if arr, ok := v.([]interface{}); ok {
			var out []int64
			for _, a := range arr {
				if f, ok := a.(float64); ok { out = append(out, int64(f)) }
			}
			return out
		}
		return nil
	}
	sDailyPNL := toFloat(raw["daily_pnl"])
	sTotalPNL := toFloat(raw["total_pnl"])
	sPeakBalance := toFloat(raw["peak_balance"])
	sProfitableDays := toInt(raw["profitable_days"])
	sTradesToday := toInt(raw["trades_today"])
	sHighestDayPNL := toFloat(raw["highest_day_pnl"])
	sGuardianStrikes := toInt(raw["guardian_strikes"])
	sLastTradeResult := toString(raw["last_trade_result"])
	sLastError := toString(raw["last_error"])
	sWins := toInt(raw["wins"])
	sLosses := toInt(raw["losses"])
	sTrackedTickets := toTickets(raw["tracked_tickets"])
	if mtime, err := os.Stat(b.stateFile); err == nil && mtime.ModTime().UTC().Day() != time.Now().UTC().Day() {
		sDailyPNL = 0
		sTradesToday = 0
		sHighestDayPNL = 0
	}
	b.state.mu.Lock()
	b.state.DailyPNL = sDailyPNL
	b.state.TotalPNL = sTotalPNL
	if sPeakBalance > 0 {
		b.state.PeakBalance = sPeakBalance
	}
	b.state.ProfitableDays = sProfitableDays
	b.state.TradesToday = sTradesToday
	b.state.HighestDayPNL = sHighestDayPNL
	b.state.GuardianStrikes = sGuardianStrikes
	b.state.LastTradeResult = sLastTradeResult
	b.state.LastError = sLastError
	b.state.Wins = sWins
	b.state.Losses = sLosses
	b.state.mu.Unlock()
	b.mu.Lock()
	for _, t := range sTrackedTickets {
		if t > 0 {
			b.trackedTickets[t] = true
		}
	}
	b.mu.Unlock()
	log.Printf("Loaded bot state: daily_pnl=$%.2f total_pnl=$%.2f wins=%d losses=%d tracked_tickets=%d", sDailyPNL, sTotalPNL, sWins, sLosses, len(sTrackedTickets))
}
func (b *TradingBot) GetStatus() map[string]interface{} {
	b.state.mu.RLock()
	defer b.state.mu.RUnlock()
	b.mu.RLock()
	profitCap := b.capConfig.ProfitLimit
	b.mu.RUnlock()
	winRate := 0.0
	if b.state.Wins+b.state.Losses > 0 {
		winRate = float64(b.state.Wins) / float64(b.state.Wins+b.state.Losses)
	}
	return map[string]interface{}{
		"running":         b.state.Running,
		"in_trade":        b.state.InTrade,
		"ticket":          b.state.Ticket,
		"daily_pnl":       b.state.DailyPNL,
		"daily_profit_cap": profitCap,
		"total_pnl":       b.state.TotalPNL,
		"peak_balance":    b.state.PeakBalance,
		"profitable_days": b.state.ProfitableDays,
		"trades_today":    b.state.TradesToday,
		"highest_day_pnl": b.state.HighestDayPNL,
		"guardian_strikes": b.state.GuardianStrikes,
		"last_trade_result": b.state.LastTradeResult,
		"last_error":      b.state.LastError,
		"last_signal":     b.state.LastSignal,
		"last_regime":     b.state.LastRegime,
		"last_confidence": b.state.LastFusionConf,
		"last_adx":        b.state.LastADX,
		"last_atr":        b.state.LastATR,
		"active_strategy": b.state.ActiveStrategy,
		"strat_wins":      b.state.StratWins,
		"strat_losses":    b.state.StratLosses,
		"strat_pnl":       b.state.StratTotalPnL,
		"wins":            b.state.Wins,
		"losses":          b.state.Losses,
		"win_rate":        winRate,
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func parseTicketFromMsg(msg string) (int64, bool) {
	idx := strings.Index(msg, "order=")
	if idx < 0 {
		return 0, false
	}
	rest := msg[idx+len("order="):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	n, err := strconv.ParseInt(rest[:end], 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}










// LabConfig is the subset of strategy parameters the bot reads from the
// Strategy Lab. Only safe-to-change parameters are included.
type LabConfig struct {
	SessionStart    int `json:"session_start"`
	SessionEnd      int `json:"session_end"`
	MaxTradesPerDay int `json:"max_trades_per_day"`
	Name            string `json:"name"`
}

// loadLabConfig reads the Strategy Lab's active strategy from disk.
// Returns nil if the file doesn't exist (first run, lab not started).
// The bot uses this to adjust session hours and trade limits based on
// the lab's research ? without importing the friday package.
func (b *TradingBot) loadLabConfig() *LabConfig {
	paths := []string{
		filepath.Join("data", "strategy_lab", "active_strategy.json"),
		filepath.Join(b.stateFile, "..", "data", "strategy_lab", "active_strategy.json"),
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var raw struct {
			ActiveConfig LabConfig `json:"active_config"`
		}
		if json.Unmarshal(data, &raw) == nil && raw.ActiveConfig.SessionEnd > 0 {
			return &raw.ActiveConfig
		}
	}
	return nil
}

// inSession checks if current UTC hour is within the trading session.
// Uses lab config if available, otherwise defaults to 12-20 UTC (London+NY).
func (b *TradingBot) inSession() bool {
	hour := time.Now().UTC().Hour()
	cfg := b.loadLabConfig()
	if cfg != nil && cfg.SessionEnd > cfg.SessionStart {
		return hour >= cfg.SessionStart && hour < cfg.SessionEnd
	}
	return hour >= 0 && hour < 20 // Combined Asian+London
}

// ApplyStrategyParams updates the TPCS strategy parameters from the lab.
// Called after Boss approves a new strategy. Only safe when no position
// is open - caller must verify.
func (b *TradingBot) ApplyStrategyParams(emaFast, emaSlow, emaPullback, rsiPeriod, atrPeriod int, slMult, tpMult float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if tpcs := b.strategy.GetTPCS(); tpcs != nil {
		tpcs.ApplyParams(emaFast, emaSlow, emaPullback, rsiPeriod, atrPeriod, slMult, tpMult)
	}
}



