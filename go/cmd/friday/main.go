package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/friday-prototype/friday-go/friday"
	"github.com/friday-prototype/friday-go/friday/trading"
	"github.com/friday-prototype/friday-go/pkg/db"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

var (
	version = "2.0.0"
	buildDate = "embedded"
)

type Provider struct {
	name     string
	url      string
	model    string
	envKey   string
	timeout  int
}

var providers = []Provider{
	{name: "github", url: "https://models.inference.ai.azure.com", model: "gpt-4o-mini", envKey: "GITHUB_TOKEN", timeout: 30},
	{name: "zai", url: "https://api.z.ai/api/paas/v4", model: "glm-4-32b-0414-128k", envKey: "ZHIPU_API_KEY", timeout: 30},
}

func maskToken(t string) string {
	if t == "" {
		return "<empty>"
	}
	if len(t) <= 8 {
		return "****"
	}
	return t[:4] + "****" + t[len(t)-4:]
}

func getEnv(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}

func mountWebUI(s *friday.Server) {
	// Resolve webui on disk from several candidates so it works no matter
	// the working directory the binary is launched from.
	candidates := []string{
		filepath.Join(friday.ProjectRoot, "..", "webui"),
		filepath.Join(friday.ProjectRoot, "webui"),
		filepath.Join(".", "webui"),
		"D:\\Friday - Prototype\\webui",
	}
	var uiDir string
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "index.html")); err == nil {
			uiDir = c
			break
		}
	}
	if uiDir == "" {
		uiDir = candidates[0] // fallback (will 404 with a clear log)
	}
	cleaned, _ := filepath.Abs(uiDir)
	s.Router().Static("/static", cleaned)

	// Control Center — single-page app at the root.
	s.Router().GET("/", func(c *gin.Context) {
		c.File(filepath.Join(cleaned, "index.html"))
	})
	s.Router().GET("/app/*any", func(c *gin.Context) {
		c.File(filepath.Join(cleaned, c.Param("any")))
	})

	log.Printf("UI: serving control center from %s", cleaned)
	return
}

func main() {
	_ = godotenv.Load()
	_ = godotenv.Load(filepath.Join(friday.ProjectRoot, "..", ".env"))
	_ = godotenv.Load(filepath.Join(friday.ProjectRoot, ".env"))

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("Friday v%s (build %s) starting...", version, buildDate)
	log.Printf("[BRIDGE] GITHUB_TOKEN loaded: %s", maskToken(os.Getenv("GITHUB_TOKEN")))

	cfg := friday.LoadConfig()

	// Initialize SQLite database
	dbPath := filepath.Join(friday.ProjectRoot, "data", "friday.db")
	if err := db.Init(dbPath); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()
	log.Printf("Database ready: %s", dbPath)

	// TEACH: persist the account-rules playbook into the decision journal
	// so Friday can recall it herself (GetDecisionsByInput) whenever a
	// "profit cap / prop firm / deploy / Exness" question comes up. Logged
	// once — guarded by a marker search so restarts don't duplicate it.
	teachCtx, teachCancel := context.WithTimeout(context.Background(), 5*time.Second)
	existing, _ := friday.GetDecisionsByInput(teachCtx, "account rules playbook", 10)
	teachCancel()
	taught := false
	for _, e := range existing {
		if strings.Contains(e.Result, "PROP FIRM PLAYBOOK v3") {
			taught = true
			break
		}
	}
	if !taught {
		playbook := `PROP FIRM PLAYBOOK v3 — know this cold.

ACCOUNTS:
- BlueGuardian (MT5 login 503985 @ BlueGuardian-Server) = PROP FIRM $5k. Rules: $150 max daily LOSS, 5% max drawdown, $250 profit TARGET, best day <= 15% of total profit (consistency), min 5 trading days. Compliance ledger: <project>/data/propfirm.json (PropFirmState.RecordTrade). 37.50/day cap = 15% of the 250 target — it protects consistency.
- Exness (login 167036042) = PERSONAL account. NO rules: no caps, 24/7 trading, only a notice below AED 10. NEVER apply prop-firm rules to Exness.
- Binance crypto grid = DORMANT until user funds it. Do not start it.

EXNESS SCALPER CONFIG (user directive, Jul 2026): autonomous Exness bot trades TPCS entries with FIXED micro-lot risk — 0.01 lots, 10-pip SL (~$1 loss), 20-pip TP (~$2 profit), 1:2 R:R, 24/7. Tunable via EXNESS_LOT / EXNESS_SL_PIPS / EXNESS_TP_PIPS env vars. Status via /trading/exness/status (fields: lot, sl_pips, tp_pips, risk_usd, reward_usd).

DAILY PROFIT CAP $37.50 (PROP_DAILY_PROFIT_CAP in .env, default 37.5; 0 = disabled):
- LAYER 1 — Entry clamp: every new prop-firm order (bot executor + /mt5/order) gets TP tightened by clampProfitTP() so TP profit = cap - $1.00 buffer. The BROKER closes at exactly that level — cannot overshoot even if the engine dies.
- LAYER 2 — Broker TP lock: engine profitLockLoop() polls every 10s; when floating profit reaches cap-5 it modifies each position's TP at the broker (TradeActionSLTP) to the lock level.
- LAYER 3 — Fire + halt: floating >= cap closes ALL positions and stops the bot for the day.
- ENTRY BLOCK: propFirmEntryBlockReason() returns a 403 for /mt5/order and the bot executor refuses. Realized PnL comes from the BALANCE LEDGER (trading/prop_ledger.json — day-start balance vs current balance; day boundary 05:00 UTC), NOT from MT5 deal-history queries: go-mt5's time-window HistoryDealsGet returns EMPTY on this terminal (only per-ticket works) — /mt5/history is therefore unreliable; trust the ledger.
- RESTART SAFETY: on startup the engine re-syncs the bot's DailyPNL and the propfirm compliance ledger from the balance ledger, so a restart can't reset the day and re-enable trading past the cap.

DEPLOY PROCEDURE (Windows server): edit ONLY in devkit drafts (devkit init "D:\Friday - Prototype\go" NAME <skips>) -> go build ./... && go test ./... -> devkit verify NAME 3 (3/3 green) -> devkit apply NAME "D:\Friday - Prototype\go" -> go build ./cmd/friday/ -> stop friday.exe -> start it -> smoke test: /status, /trading/status (daily_profit_cap), /mt5/account, /v1/models. Rollback: devkit rollback <change-id>. Journal: devkit/data/journal.jsonl.

TRADING MODE: LIVE ONLY. No paper trades, no synthetic candles, no fabricated PnL. If MT5 is unreachable: no signals, no trades, honest 503s. When asked about the account: read /mt5/account + /mt5/positions + /trading/status + trading/prop_ledger.json, never invent numbers.`
		friday.LogDecision(
			"system: account rules playbook (prop firm cap, Exness, deployment)",
			nil,
			playbook,
			"taught",
			1.0,
		)
		log.Printf("🧠 Taught Friday: account rules playbook recorded in decision journal")
	}

	// Initialize embeddings (semantic memory) — non-fatal if Ollama not running
	ollamaURL := getEnv("OLLAMA_URL", "http://localhost:11434")
	embedModel := getEnv("EMBED_MODEL", "nomic-embed-text")
	if err := friday.InitEmbeddings(ollamaURL, embedModel); err != nil {
		log.Printf("embeddings init: %v (semantic search disabled)", err)
	} else {
		log.Printf("Embeddings ready: model=%s", embedModel)
		// Backfill existing facts in background
		go func() {
			time.Sleep(5 * time.Second)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			count, err := friday.BackfillEmbeddings(ctx, 32)
			if err != nil {
				log.Printf("embedding backfill: %v", err)
			} else if count > 0 {
				log.Printf("Embedded %d existing facts", count)
			}
		}()
	}

	// Initialize Smooth Voice System
	log.Println("🧠 INITIALIZING SMOOTH VOICE SYSTEM")
	smoothVoice := friday.NewSmoothVoiceSystem()

	// Initialize Revolutionary Trading System
	log.Println("💰 INITIALIZING REVOLUTIONARY TRADING SYSTEM FOR INVESTMENT RECOVERY")
	revolutionTrading := friday.NewRevolutionTradingSystem()

	// Store in config for access
	cfg.SmoothVoice = smoothVoice
	cfg.RevolutionTrading = revolutionTrading

	// Add smooth voice handler to server
	server := friday.NewServer(cfg)
	friday.AddSmoothVoiceHandlers(server, smoothVoice)
	log.Println("✅ SMOOTH VOICE HANDLERS INTEGRATED")

	// Start activity hub + trading monitor (watches BG + Exness live)
	friday.InitActivityHub()
	friday.StartTradingMonitor(cfg.GetTradingEngineURL(), 30*time.Second)
	friday.PublishActivity("system", "Friday online", "Activity hub + trading monitor armed")
	friday.PublishActivity("monitor", "Accounts live", "BG: BlueGuardian-Server | Exness: Exness-MT5Real3")

	// Initialize standard trading engine
	engine := trading.NewEngine(cfg)
	var engineWg sync.WaitGroup
	engineWg.Add(1)
	go func() {
		defer engineWg.Done()
		if err := engine.Start(); err != nil && err != http.ErrServerClosed {
			log.Printf("Trading engine error: %v", err)
		}
	}()
	time.Sleep(500 * time.Millisecond)

	server.Router().POST("/v1/chat/completions", bridgeHandler)
	server.Router().GET("/v1/models", bridgeModelsHandler)
	mountWebUI(server)
	server.Listen()

	// Self-diagnose: test all LLM providers so Friday knows her own brain
	go func() {
		time.Sleep(2 * time.Second) // wait for server to boot
		alive := friday.SelfDiagnoseProviders()
		if len(alive) == 0 {
			friday.CreateAlert("system", "🧠 No LLM Available", "Friday cannot think — no LLM providers are responding. Check Ollama/API keys.", "critical")
		} else {
			friday.CreateAlert("system", "🧠 Friday Online", fmt.Sprintf("Brain providers: %s", strings.Join(alive, ", ")), "info")
		}
	}()

	// Proactive monitor: watches trades, generates alerts
	monitor := friday.NewProactiveMonitor()
	monitor.Start(context.Background())

	// Strategy lab: autonomous research every 6 hours
	lab := friday.GetStrategyLab()
	lab.StartResearchLoop(context.Background())

	// Signal Pipeline - autonomous signal generation and subscription service
	pipeline := friday.GetSignalPipeline()
	pipeline.Start(context.Background())

	// Marketing Engine - autonomous community discovery + outreach
	marketing := friday.GetMarketingEngine()
	marketing.Start(context.Background())

	// Crypto Payment System - real USDC payment verification from Binance wallet
	payments := friday.GetCryptoPaymentSystem()
	payments.Start(context.Background())

	// Campaign Alpha Engine - Friday's unfair advantage
	// Alpha engine removed — rebuild with arb_scanner
	_ = context.Background()
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			// Inject signals from various sources
			// This is where external signals would be fed
			log.Println("Injecting signals...")
		}
	}()

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down Friday...")
	server.Shutdown()
	engine.Stop(context.Background())
	log.Println("Friday shut down gracefully")
}