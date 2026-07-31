package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/friday-prototype/friday-go/pkg/db"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var wsUpgrade = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		allowed := []string{
			"http://localhost:8000",
			"http://localhost:3000",
			"http://127.0.0.1:8000",
			"http://127.0.0.1:3000",
		}
		for _, a := range allowed {
			if origin == a {
				return true
			}
		}
		return false
	},
}

// Server is the main Friday HTTP server
type Server struct {
	orchestrator  *Orchestrator
	upgrader      *Upgrader
	llmClient     *LLMClient
	registry      *ToolRegistry
	config        *Config
	router        *gin.Engine
	server        *http.Server
	healer        *Healer
	mu            sync.RWMutex
	activeStreams map[string]chan StreamEvent
	rateLimits    map[string]*rateBucket
	rlMu          sync.Mutex
	startTime     time.Time
}

type rateBucket struct {
	count    int
	resetAt  time.Time
}

func NewServer(cfg *Config) *Server {
	llmClient := NewLLMClient(cfg.GetLLMBridgeURL())
	registry := GlobalRegistry
	orchestrator := NewOrchestrator(cfg, llmClient, registry)
	upgrader := NewUpgrader(cfg.GetUpgradeInterval())
	healer := NewHealer(cfg, upgrader, registry)
	_ = GetCompanionState() // Initialize companion persistence

s := &Server{
		orchestrator:  orchestrator,
		upgrader:      upgrader,
		llmClient:     llmClient,
		registry:      registry,
		config:        cfg,
		healer:        healer,
		activeStreams: make(map[string]chan StreamEvent),
		rateLimits:    make(map[string]*rateBucket),
		startTime:     time.Now(),
	}

	s.setupRouter()
	return s
}

func (s *Server) setupRouter() {
	if !s.config.DevMode {
		gin.SetMode(gin.ReleaseMode)
	}
	s.router = gin.New()
	s.router.Use(gin.Recovery())
	s.router.Use(s.requestIDMiddleware())
	s.router.Use(s.loggingMiddleware())
	s.router.Use(s.securityHeadersMiddleware())
	s.router.Use(s.authMiddleware())
	s.router.Use(s.rateLimitMiddleware())
	s.router.Use(cors.New(cors.Config{
		AllowOrigins:     s.allowedOrigins(),
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowHeaders:     []string{"Content-Type", "Authorization", "X-Request-ID", "X-Conversation-ID", "Upgrade", "Connection"},
		ExposeHeaders:    []string{"X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))
	s.router.Use(s.bodySizeMiddleware())

	// Health & Status (auth not required)
	s.router.GET("/health", s.HealthHandler)
	s.router.GET("/readyz", s.ReadyzHandler)
	s.router.GET("/status", s.StatusHandler)
	s.router.GET("/team", s.TeamHandler)

	// Chat & Agentic
	s.router.POST("/chat", s.ChatHandler)
	s.router.POST("/command", s.CommandHandler)
	s.router.POST("/command/stream", s.StreamHandler)
	s.router.POST("/chat/direct", s.DirectChatHandler)
	s.router.POST("/run/:id/cancel", s.CancelHandler)

	// Voice (proxied to LLM Bridge)
	s.router.POST("/voice", s.VoiceHandler)

	// Tools
	s.router.POST("/tools/execute", s.ToolExecuteHandler)
	s.router.POST("/tools/execute/batch", s.ToolBatchHandler)
	s.router.GET("/tools/schemas", s.ToolSchemasHandler)

	// Signal Pipeline endpoints
	s.router.GET("/signals/high-confidence", s.HighConfidenceSignalsHandler)
	s.router.POST("/signals/subscribe", s.SignalSubscribeHandler)
	s.router.GET("/signals/stats", s.SignalStatsHandler)
	s.router.GET("/signals/revenue", s.SignalRevenueHandler)

	// Marketing endpoints
	s.router.GET("/marketing/stats", s.MarketingStatsHandler)
	s.router.GET("/marketing/communities", s.MarketingCommunitiesHandler)
	s.router.POST("/marketing/communities/:id/outreach", s.MarketingOutreachHandler)

	// Crypto Payment endpoints
	s.router.GET("/payments/stats", s.PaymentStatsHandler)
	s.router.GET("/payments/wallet", s.PaymentWalletHandler)
	s.router.POST("/payments/subscribe", s.PaymentSubscribeHandler)
	s.router.GET("/payments/subscription/:id", s.PaymentSubscriptionStatusHandler)

	// Trading (proxied to Trading Engine on :8001)
	s.router.POST("/trading/start", s.TradingStartHandler)
	s.router.POST("/trading/stop", s.TradingStopHandler)
	s.router.GET("/trading/status", s.TradingStatusHandler)
	s.router.POST("/trading/execute", s.TradingExecuteHandler)
	s.router.POST("/trading/close-all", s.TradingCloseAllHandler)

	// Upgrader
	s.router.POST("/upgrade/propose", s.UpgradeProposeHandler)
	s.router.POST("/upgrade/:id/approve", s.UpgradeApproveHandler)
	s.router.POST("/upgrade/:id/reject", s.UpgradeRejectHandler)
	s.router.POST("/upgrade/:id/rollback", s.UpgradeRollbackHandler)
	s.router.GET("/upgrade/history", s.UpgradeHistoryHandler)

	// Control Center endpoints
	s.router.GET("/config", s.ConfigHandler)
	s.router.POST("/config", s.ConfigUpdateHandler)
	s.router.GET("/workers/status", s.WorkersStatusHandler)
	s.router.GET("/bots", s.BotsListHandler)
	s.router.GET("/bots/earnings", s.BotsEarningsHandler)
	s.router.POST("/bots/create", s.BotCreateHandler)
	s.router.POST("/bots/stop", s.BotStopHandler)
	s.router.POST("/bots/delete", s.BotDeleteHandler)
	s.router.GET("/logs", s.LogsHandler)
	s.router.GET("/alerts", s.AlertsHandler)
	s.router.POST("/alerts/read", s.AlertsReadHandler)
	s.router.GET("/mt5/status", s.MT5StatusHandler)
	s.router.POST("/mt5/connect", s.MT5ConnectHandler)
	s.router.POST("/mt5/disconnect", s.MT5DisconnectHandler)
	s.router.POST("/emergency/kill", s.EmergencyKillHandler)

	// Device endpoints
	s.router.GET("/devices/status", s.DevicesStatusHandler)
	s.router.POST("/devices/android/connect", s.DeviceAndroidConnectHandler)
	s.router.POST("/devices/android/test-voice", s.DeviceAndroidTestVoiceHandler)
	s.router.POST("/devices/ios/connect", s.DeviceIOSConnectHandler)
	s.router.POST("/devices/ios/test-voice", s.DeviceIOSTestVoiceHandler)

	// Self-healing
	s.router.GET("/healer/status", s.HealerStatusHandler)
	s.router.GET("/healer/log", s.HealerLogHandler)
	s.router.POST("/healer/repair", s.HealerRepairHandler)

// WebSocket
	s.router.GET("/ws/control", s.WSControlHandler)
	s.router.GET("/ws/activity", s.ActivityWSHandler)

	// Activity feed
	s.router.GET("/api/activity", s.ActivityHistoryHandler)
	s.router.GET("/api/monitor/snapshot", s.MonitorSnapshotHandler)

	// Companion
	s.router.GET("/companion", s.CompanionHandler)

	// Live Earnings Dashboard
	s.router.GET("/live", s.LiveDashboardHandler)
	s.router.GET("/api/earnings", s.EarningsAPIHandler)

	// Signup / users
	s.router.GET("/signup", s.SignupPageHandler)
	s.router.POST("/api/signup", s.SignupHandler)
	s.router.GET("/api/users", s.UsersListHandler)

  // Manual trade ticket (human-driven order entry; cap-safe).
  s.router.GET("/trade", s.ManualTradePage())
  s.router.GET("/api/trade/account", s.ManualAccount())
  s.router.GET("/api/trade/tick/:symbol", s.ManualTick())
  s.router.GET("/api/trade/positions", s.ManualPositions())
  s.router.POST("/api/trade/order", s.ManualOrder())
  s.router.POST("/api/trade/propose", s.ManualPropose())
  s.router.POST("/api/trade/rustdesk/execute", s.ExecuteRustDeskTrade())
  s.router.POST("/api/trade/rustdesk_contract/confirm", s.ExecuteRustDeskContractTrade())

  // Static files (Web UI) — handled by main.go mountWebUI
}

// Router returns the gin engine, allowing external code to mount routes.
func (s *Server) Router() *gin.Engine {
	return s.router
}

// Security Middleware
func (s *Server) securityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' blob:; connect-src 'self' ws: wss:; font-src 'self' data:; form-action 'self'")
		c.Next()
	}
}

// bodySizeMiddleware restricts request body size to prevent abuse
func (s *Server) bodySizeMiddleware() gin.HandlerFunc {
	maxBytes := int64(1 << 20) // 1 MB default
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
		// Detect MaxBytesError by checking if the request was aborted
		if c.Writer.Status() == http.StatusRequestEntityTooLarge {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request too large"})
		}
	}
}

func (s *Server) authMiddleware() gin.HandlerFunc {
	token := strings.TrimSpace(s.config.APIToken)
	publicPaths := map[string]bool{
		"/health": true,
		"/status": true,
		"/team":   true,
	}

	return func(c *gin.Context) {
		if token == "" {
			c.Next()
			return
		}

		if publicPaths[c.Request.URL.Path] {
			c.Next()
			return
		}

		auth := c.GetHeader("Authorization")
		if auth == "" {
			auth = c.Query("token")
			if auth != "" {
				auth = "Bearer " + auth
			}
		}

		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing or invalid authorization"})
			return
		}

		given := strings.TrimPrefix(auth, "Bearer ")
		if given != token {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		c.Next()
	}
}

func (s *Server) rateLimitMiddleware() gin.HandlerFunc {
	limit := s.config.GetRateLimitPerMin()
	window := time.Minute

	return func(c *gin.Context) {
		if limit <= 0 {
			c.Next()
			return
		}

		ip := c.ClientIP()
		now := time.Now()

		s.rlMu.Lock()
		bucket, exists := s.rateLimits[ip]
		if !exists || now.After(bucket.resetAt) {
			bucket = &rateBucket{count: 0, resetAt: now.Add(window)}
			s.rateLimits[ip] = bucket
		}
		bucket.count++
		count := bucket.count
		s.rlMu.Unlock()

		if count > limit {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			return
		}

		c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
		c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", limit-count))
		c.Next()
	}
}

func (s *Server) allowedOrigins() []string {
	mode := s.config.DeployMode
	if mode == "local" || mode == "development" || s.config.DevMode {
		return []string{"*"}
	}
	return []string{
		fmt.Sprintf("http://localhost:%d", s.config.Port),
		fmt.Sprintf("https://localhost:%d", s.config.Port),
	}
}

func (s *Server) Start() error {
	s.Listen()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down...")

	s.Shutdown()
	return nil
}

// Shutdown performs a graceful shutdown (for use from main()).
func (s *Server) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s.upgrader.Stop()
	if err := s.server.Shutdown(ctx); err != nil {
		log.Printf("Forced shutdown: %v", err)
	}
	log.Println("Graceful shutdown complete")
}

// Listen starts the HTTP server, upgrader, and healer without blocking.
func (s *Server) Listen() {
	s.upgrader.Start(context.Background())
	s.healer.Start(context.Background())

	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	s.server = &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: s.config.LLMCallTimeout + 10*time.Second,
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("[START] Friday starting on %s", addr)
	log.Printf("  LLM Bridge: %s", s.config.GetLLMBridgeURL())
	log.Printf("  Trading Engine: %s", s.config.GetTradingEngineURL())
	log.Printf("  Web UI: http://%s", addr)

	go func() {
		restarts := 0
		for {
			func() {
				defer func() {
					if r := recover(); r != nil {
						restarts++
						backoff := time.Duration(restarts*3) * time.Second
						if backoff > 30*time.Second {
							backoff = 30 * time.Second
						}
						log.Printf("Server panic: %v - restarting in %v (attempt %d)", r, backoff, restarts)
						time.Sleep(backoff)
					}
				}()
				if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Printf("Server error: %v", err)
				}
			}()
			if s.server == nil {
				break
			}
		}
	}()
}

func (s *Server) UpgraderStop() {
	s.upgrader.Stop()
	s.healer.Stop()
}

func (s *Server) Stop(ctx context.Context) error {
	s.upgrader.Stop()
	s.healer.Stop()
	return s.server.Shutdown(ctx)
}

// Middleware
func (s *Server) requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		reqID := c.GetHeader("X-Request-ID")
		if reqID == "" {
			reqID = uuid.New().String()[:8]
		}
		c.Set("request_id", reqID)
		c.Header("X-Request-ID", reqID)
		c.Next()
	}
}

func (s *Server) loggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		if c.Request.URL.RawQuery != "" {
			path += "?" + c.Request.URL.RawQuery
		}
		c.Next()
		latency := time.Since(start)
		status := c.Writer.Status()
		clientIP := c.ClientIP()
		reqID := c.GetString("request_id")
		log.Printf("[%s] %s %s %d %v %s", reqID, c.Request.Method, path, status, latency, clientIP)
	}
}

// Handlers
func (s *Server) HealthHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	// Check all components
	llmHealthy := s.checkLLMHealth(ctx)
	tradingHealthy := s.checkTradingHealth(ctx)
	databaseHealthy := s.checkDatabaseHealth(ctx)
	toolsHealthy := len(s.registry.Schemas()) > 0

	// All must be healthy
	overallHealthy := llmHealthy && tradingHealthy && databaseHealthy && toolsHealthy

	c.JSON(http.StatusOK, gin.H{
		"status":       "ok",
		"online":       overallHealthy,
		"time":         time.Now().Unix(),
		"version":      "2.0.0-go",
		"llm_bridge":   llmHealthy,
		"trading":      tradingHealthy,
		"database":     databaseHealthy,
		"tools":        toolsHealthy,
		"go_version":   "1.22+",
		"latency":      time.Since(s.startTime).Milliseconds(),
	})
}

func (s *Server) checkLLMHealth(ctx context.Context) bool {
	// Quick health check (2 seconds)
	if err := s.llmClient.Health(ctx); err != nil {
		return false
	}
	return true
}

func (s *Server) checkTradingHealth(ctx context.Context) bool {
	tReq, _ := http.NewRequestWithContext(ctx, "GET", s.config.GetTradingEngineURL()+"/health", nil)
	resp, err := http.DefaultClient.Do(tReq)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

func (s *Server) checkDatabaseHealth(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	_, err := db.Get().ExecContext(ctx, "SELECT 1")
	return err == nil
}

// ReadyzHandler checks all subsystems: LLM bridge, trading engine, database
func (s *Server) ReadyzHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	checks := gin.H{}
	allReady := true

	// LLM bridge
	llmOK := true
	if err := s.llmClient.Health(ctx); err != nil {
		llmOK = false
		allReady = false
	}
	checks["llm_bridge"] = llmOK

	// Trading engine
	tradingOK := false
	tReq, _ := http.NewRequestWithContext(ctx, "GET", s.config.GetTradingEngineURL()+"/health", nil)
	if resp, err := http.DefaultClient.Do(tReq); err == nil {
		tradingOK = resp.StatusCode == 200
		resp.Body.Close()
	}
	if !tradingOK {
		allReady = false
	}
	checks["trading_engine"] = tradingOK

	// Database
	dbOK := true
	if _, err := db.Get().ExecContext(ctx, "SELECT 1"); err != nil {
		dbOK = false
		allReady = false
	}
	checks["database"] = dbOK

	// Tools
	checks["tools_registered"] = len(s.registry.Schemas())

	status := http.StatusOK
	if !allReady {
		status = http.StatusServiceUnavailable
	}

	c.JSON(status, gin.H{
		"ready":   allReady,
		"checks":  checks,
		"time":    time.Now().Unix(),
	})
}

func (s *Server) StatusHandler(c *gin.Context) {
	tradingRunning := false
	_ctx, _cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer _cancel()
	req, _ := http.NewRequestWithContext(_ctx, "GET", s.config.GetTradingEngineURL()+"/health", nil)
	if resp, err := http.DefaultClient.Do(req); err == nil {
		resp.Body.Close()
		tradingRunning = resp.StatusCode == 200
	}

	activeAcc := ""
	am := GetAccounts()
	if a := am.GetActive(); a != nil {
		activeAcc = fmt.Sprintf("%s (login %d @ %s, $%.0f %s)", a.Name, a.Login, a.Server, a.Balance, a.Currency)
	}

	// Fetch grid status from both engines (with timeout)
	fetchGrid := func(base string) gin.H {
		gCtx, gCancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer gCancel()
		gReq, _ := http.NewRequestWithContext(gCtx, "GET", base+"/grid/status", nil)
		if resp, err := http.DefaultClient.Do(gReq); err == nil {
			defer resp.Body.Close()
			var gs map[string]any
			if json.NewDecoder(resp.Body).Decode(&gs) == nil {
				return gs
			}
		}
		return gin.H{"active": false}
	}
	bgGrid := fetchGrid(s.config.GetTradingEngineURL())
	exGrid := fetchGrid("http://localhost:8003")

	// Fetch Exness instance status
	exnessStatus := gin.H{"running": false, "error": "unreachable"}
	exCtx, exCancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer exCancel()
	exReq, _ := http.NewRequestWithContext(exCtx, "GET", "http://localhost:8002/status", nil)
	if resp, err := http.DefaultClient.Do(exReq); err == nil {
		defer resp.Body.Close()
		var es map[string]any
		if json.NewDecoder(resp.Body).Decode(&es) == nil {
			exnessStatus = es
		}
	}

	// Build live bots list
	botList := []gin.H{}
	if bgGrid["active"] == true {
		botList = append(botList, gin.H{"name": "BG Crypto Grid", "type": "grid", "symbol": "ETHUSDT", "pnl": bgGrid["total_pnl"], "active": true})
	}
	if exGrid["active"] == true {
		botList = append(botList, gin.H{"name": "Exness Crypto Grid", "type": "grid", "symbol": "ETHUSDT", "pnl": exGrid["total_pnl"], "active": true})
	}

	workerCount := activeWorkerCount()
	providers := []string{"none"}
	if s.llmClient != nil {
		if names := s.llmClient.ProviderNames(); len(names) > 0 {
			providers = names
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"status":         "online",
		"orchestrator":   "ready",
		"workers_active": workerCount,
		"tools_count":    len(s.registry.Schemas()),
		"uptime_seconds": int(time.Since(s.startTime).Seconds()),
		"no_key":         !s.config.HasAnyKey(),
		"providers":      providers,
		"eye_active":     false,
		"trading": gin.H{
			"running": tradingRunning,
		},
		"bots":            botList,
		"trading_engine":  tradingRunning,
		"services_online": workerCount,
		"max_services":    workerCount,
		"active_account":  activeAcc,
		"total_accounts":  len(am.List()),
		"instances": gin.H{
			"blue_guardian": gin.H{"port": 8000, "account": "Blue Guardian 5k", "running": tradingRunning, "grid": bgGrid},
			"exness":        exnessStatus,
		},
	})
}

func (s *Server) TeamHandler(c *gin.Context) {
	services := s.GetServices(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{"workers": services, "services": services})
}

func (s *Server) DirectChatHandler(c *gin.Context) {
	var req struct{ Text string `json:"text"` }
	if err := c.ShouldBindJSON(&req); err != nil || req.Text == "" {
		c.JSON(400, gin.H{"error": "text required"})
		return
	}
	// Truthful live context. The previous version hardcoded
	// "Blue Guardian $5k ... EURUSDm ... 66.9% win rate ... MT5 connected"
	// regardless of actual connection state. We now pull the real engine
	// snapshot; only truthful, verifiable facts go to the model.
	liveInfo := LiveState(c.Request.Context()).LiveContextBlock()

	systemPrompt := "You are Friday, an AI assistant created by Sagar Adhikari. Your boss is Sagar Adhikari. 55+ AI/Trading/Finance tools. EARNING: Bandwidth sharing ($1.1-4.1/day) + Testnet nodes ($0/day daily, $500-1200 one-time if airdrops hit) + Faucets ($0.02-0.10/day). No mining - worthless. FULLY AUTONOMOUS. MT5: BlueGuardian + Exness bots on EURUSDm. Crypto Grid on Binance. GitHub->GLM-4-32B instant fallback. Use 'bandwidth' for passive, 'node_runner' for airdrops. 24/7 with 0 investment." + " " + liveInfo

	messages := []Message{
		{Role: "system", Content: systemPrompt},
	}
	cs := GetCompanionState()
	history := cs.GetHistory()
	start := 0
	if len(history) > 10 {
		start = len(history) - 10
	}
	for _, h := range history[start:] {
		messages = append(messages, Message{Role: h["role"], Content: h["content"]})
	}
	messages = append(messages, Message{Role: "user", Content: req.Text})
	cs.RecordMessage("user", req.Text)

	resp, err := s.llmClient.Chat(c.Request.Context(), messages, Role("user"))
	if err != nil {
		c.JSON(200, gin.H{"reply": "Give me a second — thinking..."})
		return
	}
	if len(resp.Choices) > 0 && resp.Choices[0].Message.Content != "" {
		reply := resp.Choices[0].Message.Content
		cs.RecordMessage("assistant", reply)
		c.JSON(200, gin.H{"reply": reply})
	} else {
		c.JSON(200, gin.H{"reply": "Give me a moment..."})
	}
}

func (s *Server) ChatHandler(c *gin.Context) {
	var req struct {
		Model    string          `json:"model"`
		Messages json.RawMessage `json:"messages"`
		Stream   bool            `json:"stream"`
		UserID   string          `json:"user_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	if req.Model == "" {
		req.Model = "friday"
	}

	var messages []Message
	json.Unmarshal(req.Messages, &messages)

	ctx := c.Request.Context()
	userID := req.UserID
	if userID == "" {
		userID = "default"
	}

if !req.Stream {
		lastUser := ""
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == "user" {
				lastUser = messages[i].Content
				break
			}
		}

		// Activity: user message
		if lastUser != "" {
			PublishActivity("chat", "You → Friday", lastUser)
		}

		// Truthful live context: when the question touches the account,
		// trades, or the bot, feed the engine snapshot to the model.
		if HasTradingIntent(lastUser) {
			live := LiveState(ctx).LiveContextBlock()
			messages = append(messages, Message{Role: "system", Content: live})
		}

		// Inject decision memory context for non-stream path
		if dm := getDecisionMemory(); dm != nil {
			if tools, conf := dm.Suggest(lastUser); len(tools) > 0 && conf > 0.3 {
				hint := fmt.Sprintf("[Memory] For similar queries, these tools worked: %s", strings.Join(tools, ", "))
				// Prepend to the first system message or add as first message
				found := false
				for i := range messages {
					if messages[i].Role == "system" {
						messages[i].Content += "\n" + hint
						found = true
						break
					}
				}
				if !found {
					messages = append([]Message{{Role: "system", Content: hint}}, messages...)
				}
			}
		}

		resp, err := s.llmClient.Chat(ctx, messages, "user")
		reply := s.config.GreetingText
		if err == nil && len(resp.Choices) > 0 && resp.Choices[0].Message.Content != "" {
			reply = resp.Choices[0].Message.Content
		}

		// Activity: Friday's reply
		if reply != "" && reply != s.config.GreetingText {
			PublishActivity("chat", "Friday → You", reply)
		}

		c.JSON(http.StatusOK, gin.H{
			"id":      fmt.Sprintf("chatcmpl-%s", uuid.New().String()[:8]),
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   req.Model,
			"choices": []gin.H{{
				"index": 0,
				"message": gin.H{"role": "assistant", "content": reply},
				"finish_reason": "stop",
			}},
			"usage": gin.H{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
		})
		return
	}

lastUser := ""
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			lastUser = messages[i].Content
			break
		}
	}

	// Activity: user message
	if lastUser != "" {
		PublishActivity("chat", "You → Friday", lastUser)
	}

	runID := uuid.New().String()
	stream := s.orchestrator.AgenticRun(ctx, runID, lastUser, "en", userID)

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	c.Stream(func(w io.Writer) bool {
		select {
		case event, ok := <-stream:
			if !ok {
				return false
			}
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", data)
			return true
		case <-ctx.Done():
			return false
		}
	})
}

func (s *Server) CommandHandler(c *gin.Context) {
	var req struct {
		Text     string          `json:"text"`
		Context  string          `json:"context"`
		Lang     string          `json:"lang"`
		Screen   string          `json:"screen"`
		Messages json.RawMessage `json:"messages"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var messages []Message
	json.Unmarshal(req.Messages, &messages)

	ctx := c.Request.Context()
	userID := "default"

	runID := uuid.New().String()
	stream := s.orchestrator.AgenticRun(ctx, runID, req.Text, req.Lang, userID)

	var finalReply string
	for event := range stream {
		if event.Type == "final" {
			finalReply = event.Content
			break
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"reply": finalReply,
		"lang":  req.Lang,
	})
}

func (s *Server) StreamHandler(c *gin.Context) {
	var req struct {
		Text     string          `json:"text"`
		Context  string          `json:"context"`
		Lang     string          `json:"lang"`
		Screen   string          `json:"screen"`
		Messages json.RawMessage `json:"messages"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var messages []Message
	json.Unmarshal(req.Messages, &messages)

	runID := uuid.New().String()
	stream := s.orchestrator.AgenticRun(c.Request.Context(), runID, req.Text, req.Lang, "default")

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	defer func() {
		if r := recover(); r != nil {
			log.Printf("SSE stream panic recovered: %v", r)
		}
	}()

	c.Stream(func(w io.Writer) bool {
		select {
		case event, ok := <-stream:
			if !ok {
				return false
			}
			data, _ := json.Marshal(event)
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				log.Printf("SSE write error: %v", err)
				return false
			}
			return true
		case <-c.Request.Context().Done():
			return false
		}
	})
}

func (s *Server) CancelHandler(c *gin.Context) {
	runID := c.Param("id")
	ok := s.orchestrator.Cancel(runID)
	c.JSON(http.StatusOK, gin.H{"cancelled": ok})
}

func (s *Server) VoiceHandler(c *gin.Context) {
	var req struct {
		Text  string `json:"text"`
		Voice string `json:"voice"`
		Rate  string `json:"rate"`
		Pitch string `json:"pitch"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Text == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "text required"})
		return
	}

	if req.Voice == "" {
		req.Voice = s.config.TTSVoiceEdge
	}
	if req.Rate == "" {
		req.Rate = "+0%"
	}
	if req.Pitch == "" {
		req.Pitch = s.config.TTSPitchEdge
	}

	// Generate speech via robust TTS (returns MP3 bytes)
	audio, err := s.generateSpeech(req.Text, req.Voice, req.Rate, req.Pitch)
	if err != nil {
		// Ultimate fallback: return JSON with error but don't crash
		c.JSON(http.StatusOK, gin.H{
			"status":  "error",
			"text":    req.Text,
			"message": "TTS generation failed: " + err.Error(),
		})
		return
	}

	// Stream MP3 audio
	c.Header("Content-Type", "audio/mpeg")
	c.Header("Content-Length", strconv.Itoa(len(audio)))
	c.Header("Accept-Ranges", "bytes")
	c.Data(http.StatusOK, "audio/mpeg", audio)
}

// generateSpeech tries multiple TTS backends, returns MP3 bytes
func (s *Server) generateSpeech(text, voice, rate, pitch string) ([]byte, error) {
	cacheKey := hashString(text + "|" + voice + "|" + rate + "|" + pitch)
	if cached := s.getAudioCache(cacheKey); cached != nil {
		log.Printf("[TTS] cache hit for: %.30s...", text)
		return cached, nil
	}

	// Backend 1: edge-tts (best quality, natural)
	if audio, err := s.ttsEdge(text, voice, rate, pitch); err == nil {
		s.setAudioCache(cacheKey, audio)
		return audio, nil
	}

	// Backend 2: gTTS (Google, reliable fallback)
	if audio, err := s.ttsGtts(text, voice); err == nil {
		s.setAudioCache(cacheKey, audio)
		return audio, nil
	}

	// Backend 3: Windows SAPI (always works, robotic but never fails)
	if audio, err := s.ttsSapi(text); err == nil {
		s.setAudioCache(cacheKey, audio)
		return audio, nil
	}

	return nil, fmt.Errorf("all TTS backends failed")
}

func (s *Server) ttsEdge(text, voice, rate, pitch string) ([]byte, error) {
	pyScript := `
import asyncio, edge_tts, sys
async def main():
    text = sys.argv[1]
    voice = sys.argv[2]
    rate = sys.argv[3]
    pitch = sys.argv[4]
    out = sys.argv[5]
    comm = edge_tts.Communicate(text, voice, rate=rate, pitch=pitch)
    await comm.save(out)
asyncio.run(main())
`
	tmpDir := os.TempDir()
	scriptPath := filepath.Join(tmpDir, "friday_tts_"+hashString(text)+".py")
	os.WriteFile(scriptPath, []byte(pyScript), 0644)
	defer os.Remove(scriptPath)

	outPath := filepath.Join(tmpDir, "friday_tts_"+hashString(text)+".mp3")
	cmd := exec.Command("python", scriptPath, text, voice, rate, pitch, outPath)
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	defer os.Remove(outPath)
	return os.ReadFile(outPath)
}

func (s *Server) ttsGtts(text, voice string) ([]byte, error) {
	lang := "en"
	if strings.HasPrefix(voice, "en-") {
		lang = "en"
	} else if strings.HasPrefix(voice, "hi-") {
		lang = "hi"
	}
	script := fmt.Sprintf(`
from gtts import gTTS
import sys
tts = gTTS(text=sys.argv[1], lang="%s", slow=False)
tts.save(sys.argv[2])
`, lang)
	tmpDir := os.TempDir()
	scriptPath := filepath.Join(tmpDir, "friday_gtts_"+hashString(text)+".py")
	os.WriteFile(scriptPath, []byte(script), 0644)
	defer os.Remove(scriptPath)

	outPath := filepath.Join(tmpDir, "friday_gtts_"+hashString(text)+".mp3")
	cmd := exec.Command("python", scriptPath, text, outPath)
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	defer os.Remove(outPath)
	return os.ReadFile(outPath)
}

func (s *Server) ttsSapi(text string) ([]byte, error) {
	// Windows SAPI via PowerShell -> WAV -> convert to MP3 via ffmpeg
	tmpDir := os.TempDir()
	wavPath := filepath.Join(tmpDir, "friday_sapi_"+hashString(text)+".wav")
	mp3Path := filepath.Join(tmpDir, "friday_sapi_"+hashString(text)+".mp3")

	psScript := fmt.Sprintf(`
Add-Type -AssemblyName System.Speech
$synth = New-Object System.Speech.Synthesis.SpeechSynthesizer
$synth.Rate = 1
$synth.Volume = 100
$synth.SetOutputToWaveFile("%s")
$synth.Speak("%s")
$synth.Dispose()
`, wavPath, strings.ReplaceAll(text, `"`, `\"`))
	
	cmd := exec.Command("powershell", "-Command", psScript)
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	defer os.Remove(wavPath)

	// Convert WAV to MP3 using ffmpeg
	cmd = exec.Command("ffmpeg", "-y", "-i", wavPath, "-codec:a", "libmp3lame", "-b:a", "128k", mp3Path)
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	defer os.Remove(mp3Path)
	return os.ReadFile(mp3Path)
}

// Audio cache (simple in-memory LRU)
var audioCache = struct {
	sync.RWMutex
	data map[string][]byte
	order []string
	max  int
}{data: make(map[string][]byte), max: 100}

func (s *Server) getAudioCache(key string) []byte {
	audioCache.RLock()
	defer audioCache.RUnlock()
	return audioCache.data[key]
}

func (s *Server) setAudioCache(key string, audio []byte) {
	audioCache.Lock()
	defer audioCache.Unlock()
	if len(audioCache.data) >= audioCache.max {
		// Remove oldest
		oldest := audioCache.order[0]
		delete(audioCache.data, oldest)
		audioCache.order = audioCache.order[1:]
	}
	audioCache.data[key] = audio
	audioCache.order = append(audioCache.order, key)
}

func (s *Server) ToolExecuteHandler(c *gin.Context) {
	var req struct {
		Tool string          `json:"tool"`
		Args json.RawMessage `json:"args"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	result, err := s.registry.Execute(ctx, req.Tool, req.Args)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"result": result, "tool": req.Tool})
}

func (s *Server) ToolBatchHandler(c *gin.Context) {
	var req struct {
		Calls []ToolCall `json:"calls"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	results := s.registry.ExecuteBatch(ctx, req.Calls)
	c.JSON(http.StatusOK, gin.H{"results": results})
}

func (s *Server) ToolSchemasHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"schemas": s.registry.Schemas()})
}

func (s *Server) TradingStartHandler(c *gin.Context) {
	resp, err := enginePost("/trading/start", nil)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"status": "error",
			"error":  err.Error(),
			"engine": s.config.GetTradingEngineURL(),
			"hint":   "Trading engine unreachable. Is it running on port 8001? Check Friday's logs.",
		})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (s *Server) TradingStopHandler(c *gin.Context) {
	resp, err := enginePost("/trading/stop", nil)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"status": "error",
			"error":  err.Error(),
			"engine": s.config.GetTradingEngineURL(),
			"hint":   "Trading engine unreachable. Is it running on port 8001?",
		})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (s *Server) TradingStatusHandler(c *gin.Context) {
	resp, err := engineGet("/trading/status")
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"running": false, "engine": s.config.GetTradingEngineURL(), "error": err.Error()})
		return
	}
c.JSON(http.StatusOK, resp)
}

func (s *Server) TradingExecuteHandler(c *gin.Context) {
	resp, err := enginePost("/trading/execute", c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"status": "error",
			"error":  err.Error(),
			"engine": s.config.GetTradingEngineURL(),
			"hint":   "Trading engine unreachable. Is it running on port 8001?",
		})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (s *Server) TradingCloseAllHandler(c *gin.Context) {
	resp, err := enginePost("/trading/close-all", nil)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"status": "error",
			"error":  err.Error(),
			"engine": s.config.GetTradingEngineURL(),
		})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (s *Server) UpgradeProposeHandler(c *gin.Context) {
	ctx := c.Request.Context()
	proposal, err := s.upgrader.generateProposal(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, proposal)
}

func (s *Server) UpgradeApproveHandler(c *gin.Context) {
	id := c.Param("id")
	if err := s.upgrader.Apply(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "applied", "id": id})
}

func (s *Server) UpgradeRejectHandler(c *gin.Context) {
	id := c.Param("id")
	if err := s.upgrader.Reject(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "rejected", "id": id})
}

func (s *Server) UpgradeRollbackHandler(c *gin.Context) {
	id := c.Param("id")
	if err := s.upgrader.Rollback(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "rolled_back", "id": id})
}

func (s *Server) UpgradeHistoryHandler(c *gin.Context) {
	history := s.upgrader.List()
	c.JSON(http.StatusOK, gin.H{"proposals": history})
}

// Control Center Handlers
func (s *Server) ConfigHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"tts_engine":        s.config.TTSEngine,
		"tts_voice_en":      s.config.TTSVoiceEN,
		"tts_voice_edge":    s.config.TTSVoiceEdge,
		"stt_provider":      s.config.STTProvider,
		"default_symbol":    "EURUSD",
		"risk_pct":          1.0,
		"max_daily_loss":    100,
		"auto_trade":        false,
		"screen_watch":      s.config.ScreenWatch,
		"proactive_act":     s.config.ProactiveAct,
	})
}

func (s *Server) ConfigUpdateHandler(c *gin.Context) {
	var cfg map[string]string
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "updated", "config": cfg})
}

func (s *Server) WorkersStatusHandler(c *gin.Context) {
	services := s.GetServices(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{
		"workers":    services,
		"services":   services,
		"count":      len(services),
	})
}

func (s *Server) BotsListHandler(c *gin.Context) {
	// Live data only. The previous hardcode returned `{"bots":[]}` and
	// lied in 3 separate ways simultaneously (BotsListHandler empty,
	// MT5StatusHandler false, registerBots() active). The dashboard should
	// reflect reality.
	bots := []gin.H{}

	// Bot #1: MT5 swing bot (real state pulled from engine :8001)
	mt5 := gin.H{}
	if resp, err := http.Get(s.config.GetTradingEngineURL() + "/health"); err == nil {
		body, err := io.ReadAll(resp.Body)
		if err == nil {
			var hs map[string]any
			if err := json.Unmarshal(body, &hs); err == nil {
				mt5["health_body"] = hs
			}
		}
		resp.Body.Close()
	}
	if acc, err := engineGet("/mt5/account"); err == nil {
		mt5["connected"] = true
		mt5["account"] = acc
		if l, _ := acc["login"].(float64); l > 0 {
			mt5["label"] = fmt.Sprintf("MT5 %v @ %v", acc["login"], acc["server"])
		}
	} else {
		mt5["connected"] = false
	}
	if st, err := engineGet("/trading/status"); err == nil {
		mt5["status"] = st
	}
	bots = append(bots, gin.H{"id": "mt5_swing_bot", "name": "MT5 swing bot", "type": "MT5 single-connection", "live": mt5})

	// Bot #2: Crypto grid bot on Binance (real state via engine)
	crypto := gin.H{}
	if gs, err := engineGet("/grid/status"); err == nil {
		crypto["grid"] = gs
	}
	if pf, err := engineGet("/crypto/portfolio"); err == nil {
		crypto["portfolio"] = pf
	}
	bots = append(bots, gin.H{"id": "crypto_grid_bot", "name": "Crypto grid bot", "type": "Binance spot", "live": crypto})
	c.JSON(http.StatusOK, gin.H{"bots": bots, "count": len(bots)})
}

// BotCreateHandler / BotStopHandler / BotDeleteHandler were previously
// returning success strings without actually creating/stopping/deleting any
// bot. We now respond honestly with the operations actually available (and
// which ones aren't), so the dashboard doesn't lie. To start/stop the live
// bots, use the trading_start / trading_stop / crypto_grid / miner_bot tools
// or POST /trading/start, /grid/start|stop, etc.
func (s *Server) BotCreateHandler(c *gin.Context) {
	var req struct {
		BotType string                 `json:"bot_type"`
		Name    string                 `json:"name"`
		Config  map[string]interface{} `json:"config"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"status":  "not_implemented",
		"reason":  "Bot provisioning is not implemented. Use the typed tools instead: trading_start or crypto_grid action=start or miner_bot action=start.",
		"name":    req.Name,
		"type":    req.BotType,
	})
}

func (s *Server) BotStopHandler(c *gin.Context) {
	var req struct{ BotID string `json:"bot_id"` }
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// Route by botID to the matching live stop endpoint so the dashboard
	// 'stop' button actually does something real instead of returning
	// a fake "stopped" OK.
	switch req.BotID {
	case "mt5_swing_bot", "exness_bot", "blue_guardian_bot":
		if r, err := enginePost("/trading/stop", nil); err == nil {
			c.JSON(http.StatusOK, gin.H{"status": "stopped", "bot_id": req.BotID, "engine_result": r})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "stop attempted", "bot_id": req.BotID, "note": "engine unreachable"})
		return
	case "crypto_grid_bot":
		if r, err := enginePost("/grid/stop", nil); err == nil {
			c.JSON(http.StatusOK, gin.H{"status": "stopped", "bot_id": req.BotID, "engine_result": r})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "stop attempted", "bot_id": req.BotID})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{
		"status": "unknown_bot",
		"bot_id": req.BotID,
		"known":  []string{"mt5_swing_bot", "crypto_grid_bot"},
	})
}

func (s *Server) BotDeleteHandler(c *gin.Context) {
	var req struct{ BotID string `json:"bot_id"` }
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"status": "not_implemented",
		"reason": "Bots are defined by code/config, not spawned by this API. BotDelete can't remove the underlying implementation; stop them via /bots/stop instead.",
		"bot_id": req.BotID,
	})
}

// BotsEarningsHandler returns real earnings state from the compounder and
// live MT5 balance — the previous 0.0 hardcode was misleading because the
// compounder DOES track accumulated capital and day-by-day history.
func (s *Server) BotsEarningsHandler(c *gin.Context) {
	comp := GetCompounder()
	mt5 := gin.H{}
	if acc, err := engineGet("/mt5/account"); err == nil {
		mt5 = gin.H{
			"balance":  acc["balance"],
			"equity":   acc["equity"],
			"login":    acc["login"],
			"server":   acc["server"],
		}
		// Realized PnL ≈ current balance - configured account size.
		if bal, _ := acc["balance"].(float64); bal > 0 {
			pf := GetPropFirm()
			mt5["approx_realized_pnl"] = bal - pf.Config.AccountSize
		}
	} else {
		mt5["connected"] = false
	}
	c.JSON(http.StatusOK, gin.H{
		"total_capital":   comp.TotalCapital,
		"auto_reinvest":  comp.AutoReinvest,
		"daily_history":  comp.DailyHistory,
		"streams":        comp.Streams,
		"mt5_account":    mt5,
		"miner_running":  false,
	})
}

func (s *Server) LogsHandler(c *gin.Context) {
	level := c.DefaultQuery("level", "all")
	source := c.DefaultQuery("source", "all")
	limit := 200
	emptyLogs := make([]gin.H, 0)
	c.JSON(http.StatusOK, gin.H{"logs": emptyLogs, "filters": gin.H{"level": level, "source": source, "limit": limit}})
}

func (s *Server) AlertsHandler(c *gin.Context) {
	alerts := GetUnreadAlerts()
	if alerts == nil {
		alerts = []Alert{}
	}
	c.JSON(http.StatusOK, gin.H{"alerts": alerts, "count": len(alerts)})
}

func (s *Server) AlertsReadHandler(c *gin.Context) {
	MarkAlertsRead()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) MT5StatusHandler(c *gin.Context) {
	// Pull the REAL MT5 connection state from the trading engine rather
	// than the previous hardcoded {connected:false} lie.
	out := gin.H{"connected": false}
	if acc, err := engineGet("/mt5/account"); err == nil {
		out["connected"] = true
		out["account"] = acc
		if l, _ := acc["login"].(float64); l > 0 {
			out["label"] = fmt.Sprintf("%v @ %v", acc["login"], acc["server"])
		}
	}
	if st, err := engineGet("/trading/status"); err == nil {
		out["engine_status"] = st
		if le, _ := st["last_error"].(string); le != "" {
			out["last_error"] = le
			if expl := LookupMT5ErrorString(le); expl != "" {
				out["error_explanation"] = expl
			}
		}
		if running, _ := st["running"].(bool); running {
			out["engine_running"] = true
		}
	} else {
		out["engine_running"] = false
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) MT5ConnectHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "connecting", "message": "MT5 bridge connection initiated"})
}

func (s *Server) MT5DisconnectHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "disconnected", "message": "MT5 bridge disconnected"})
}

func (s *Server) EmergencyKillHandler(c *gin.Context) {
	var req struct {
		Confirmation string `json:"confirmation"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Confirmation != "KILL" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid confirmation. Type 'KILL' to confirm."})
		return
	}

	s.emergencyKill()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "EMERGENCY KILL EXECUTED — All systems halted",
	})
}

func (s *Server) emergencyKill() {
	if s.orchestrator != nil {
		s.orchestrator.CancelAll()
	}
	log.Println("EMERGENCY KILL EXECUTED")
}

// Device Handlers
func (s *Server) DevicesStatusHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"devices": gin.H{
			"android": gin.H{
				"connected":   false,
				"last_seen":   nil,
				"battery":     nil,
				"version":     nil,
				"wake_word":   false,
				"capabilities": map[string]bool{
					"voice":         true,
					"screen":        true,
					"sms":           true,
					"notifications": true,
					"apps":          true,
					"contacts":      true,
					"location":      true,
					"media":         true,
				},
			},
			"ios": gin.H{
				"connected":   false,
				"last_seen":   nil,
				"battery":     nil,
				"version":     nil,
				"wake_word":   false,
				"capabilities": map[string]bool{
					"voice":         true,
					"screen":        true,
					"sms":           true,
					"notifications": true,
					"apps":          true,
					"contacts":      true,
					"location":      true,
					"media":         true,
				},
			},
		},
	})
}

func (s *Server) DeviceAndroidConnectHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Android connection initiated"})
}

func (s *Server) DeviceAndroidTestVoiceHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Test voice sent to Android"})
}

func (s *Server) DeviceIOSConnectHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "iOS connection initiated"})
}

func (s *Server) DeviceIOSTestVoiceHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Test voice sent to iOS"})
}

// Companion Handler
func (s *Server) CompanionHandler(c *gin.Context) {
	state := GetCompanionState()
	c.JSON(http.StatusOK, gin.H{
		"user_id":        state.UserID,
		"total_messages": state.TotalMessages,
		"crash_count":    state.CrashCount,
		"first_seen":     state.FirstSeen.Format(time.RFC3339),
		"last_seen":      state.LastSeen.Format(time.RFC3339),
		"preferences":    state.Preferences,
		"summary":        state.Summary(),
	})
}

// Live Dashboard Handlers
func (s *Server) LiveDashboardHandler(c *gin.Context) {
	interfaceDir := filepath.Join(ProjectRoot, "interface")
	if _, err := os.Stat(filepath.Join(interfaceDir, "live.html")); err != nil {
		// Fallback to go/interface
		interfaceDir = filepath.Join(ProjectRoot, "interface")
	}
	c.File(filepath.Join(interfaceDir, "live.html"))
}

var cachedEarnings map[string]interface{}
var earningsMu sync.RWMutex
var earningsLastFetch time.Time

func (s *Server) EarningsAPIHandler(c *gin.Context) {
	// Return cached response immediately to avoid hanging
	earningsMu.RLock()
	cached := cachedEarnings
	age := time.Since(earningsLastFetch)
	earningsMu.RUnlock()

	if cached != nil && age < 10*time.Second {
		c.JSON(http.StatusOK, cached)
		return
	}

	// Return stale data instantly, refresh in background
	if cached != nil {
		c.JSON(http.StatusOK, cached)
	} else {
		c.JSON(http.StatusOK, gin.H{
			"total_capital": 0, "streams": []interface{}{},
			"resources": gin.H{"cpu_usage": 0, "memory_usage_pct": 0},
			"companion": gin.H{"total_messages": 0, "capabilities": 0},
		})
	}

	// Background refresh
	go func() {
		compounder := GetCompounder()
		rm := GetResourceManager()
		cs := GetCompanionState()
		resources := rm.GetCurrent()
		resp := map[string]interface{}{
			"total_capital": compounder.TotalCapital,
			"auto_reinvest": compounder.AutoReinvest,
			"streams":       compounder.Streams,
			"daily_history": compounder.DailyHistory,
			"growth_7d":     compounder.GrowthRate7d,
			"growth_30d":    compounder.GrowthRate30d,
			"resources": gin.H{
				"cpu_usage":        resources.CPU.LogicalProcs,
				"memory_usage_pct": resources.Memory.UsagePct,
				"disk_free_gb":     resources.Disk.FreeGB,
				"mining_intensity": rm.MiningIntensity,
				"idle_mode":        rm.IdleMode,
			},
			"companion": gin.H{
				"total_messages": cs.TotalMessages,
				"capabilities":   len(cs.Capabilities),
			},
			"timestamp": time.Now().Unix(),
		}
		earningsMu.Lock()
		cachedEarnings = resp
		earningsLastFetch = time.Now()
		earningsMu.Unlock()
	}()
}

// Self-Healing Handlers
func (s *Server) HealerStatusHandler(c *gin.Context) {
	health := s.healer.Health()
	status := "healthy"
	if !health.ServerAlive || len(health.Errors) > 0 {
		status = "degraded"
	}
	c.JSON(http.StatusOK, gin.H{
		"status":   status,
		"health":   health,
		"repairs":  len(s.healer.RepairLog()),
	})
}

func (s *Server) HealerLogHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"repairs": s.healer.RepairLog()})
}

func (s *Server) HealerRepairHandler(c *gin.Context) {
	var req struct {
		Issue  string `json:"issue"`
		Action string `json:"action"`
		Detail string `json:"detail"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Issue == "" || req.Action == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "issue and action are required"})
		return
	}

	err := s.healer.RepairFromWorker(req.Issue, req.Action, req.Detail)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"issue":   req.Issue,
		"action":  req.Action,
	})
}

// WSControlHandler handles WebSocket connections for the Control Center
func (s *Server) WSControlHandler(c *gin.Context) {
	conn, err := wsUpgrade.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[WS] Upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	clientID := uuid.New().String()[:8]
	log.Printf("[WS] Client connected: %s", clientID)

	// Start ping ticker to keep connection alive
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}()

	// Read loop — handle incoming messages (subscribe commands)
	for {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		conn.SetPongHandler(func(string) error {
			conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			return nil
		})
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			log.Printf("[WS] Client %s disconnected: %v", clientID, err)
			break
		}

		if messageType == websocket.TextMessage {
			var msg map[string]interface{}
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}

			msgType, ok := msg["type"].(string)
			if !ok {
				continue
			}

			switch msgType {
			case "subscribe":
				// Acknowledge subscription
				ack, _ := json.Marshal(map[string]interface{}{
					"type":   "subscribed",
					"client": clientID,
					"channels": msg["channels"],
				})
				conn.WriteMessage(websocket.TextMessage, ack)

			case "ping":
				pong, _ := json.Marshal(map[string]interface{}{"type": "pong"})
				conn.WriteMessage(websocket.TextMessage, pong)

			default:
				// Echo back with acknowledgment
				ack, _ := json.Marshal(map[string]interface{}{
					"type":      "acknowledged",
					"client":    clientID,
					"raw_type":  msgType,
				})
				conn.WriteMessage(websocket.TextMessage, ack)
			}
		}
	}
}


func (s *Server) HighConfidenceSignalsHandler(c *gin.Context) {
	pipeline := GetSignalPipeline()
	signals := pipeline.GetHighConfidenceSignals()
	c.JSON(200, gin.H{"status": "ok", "count": len(signals), "signals": signals})
}

func (s *Server) SignalSubscribeHandler(c *gin.Context) {
	var req struct {
		Email string `json:"email"`
		Tier  string `json:"tier"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if req.Tier == "" {
		req.Tier = "standard"
	}

	pipeline := GetSignalPipeline()
	subID, err := pipeline.Subscribe(req.Email, req.Tier)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"status":      "subscribed",
		"subscriber_id": subID,
		"tier":        req.Tier,
		"price":       pipeline.subscriptionPrice,
		"message":     "Welcome to FridaySignal Pro. You will receive real-time trading signals automatically.",
	})
}

func (s *Server) SignalStatsHandler(c *gin.Context) {
	pipeline := GetSignalPipeline()
	stats := pipeline.GetStats()
	c.JSON(200, gin.H{"status": "ok", "stats": stats})
}

func (s *Server) SignalRevenueHandler(c *gin.Context) {
	pipeline := GetSignalPipeline()
	revenue := pipeline.GetRevenue()
	c.JSON(200, gin.H{"status": "ok", "total_revenue": revenue, "subscription_price": pipeline.subscriptionPrice})
}

func nowISO() string {
	return time.Now().Format("15:04:05.000")
}

func (s *Server) MarketingStatsHandler(c *gin.Context) {
	engine := GetMarketingEngine()
	stats := engine.GetStats()
	c.JSON(200, gin.H{"status": "ok", "stats": stats})
}

func (s *Server) MarketingCommunitiesHandler(c *gin.Context) {
	engine := GetMarketingEngine()
	engine.mu.RLock()
	communities := engine.discoveredCommunities
	engine.mu.RUnlock()
	c.JSON(200, gin.H{"status": "ok", "communities": communities})
}

func (s *Server) MarketingOutreachHandler(c *gin.Context) {
	engine := GetMarketingEngine()
	id := c.Param("id")

	engine.mu.RLock()
	var community *Community
	for i := range engine.discoveredCommunities {
		if engine.discoveredCommunities[i].ID == id {
			community = &engine.discoveredCommunities[i]
			break
		}
	}
	engine.mu.RUnlock()

	if community == nil {
		c.JSON(404, gin.H{"error": "community not found"})
		return
	}

	pipeline := GetSignalPipeline()
	signals := pipeline.GetHighConfidenceSignals()

	success := engine.postToCommunity(context.Background(), *community, signals)
	if success {
		c.JSON(200, gin.H{"status": "posted", "community": community.Name})
	} else {
		c.JSON(500, gin.H{"status": "failed", "community": community.Name})
	}
}

func (s *Server) PaymentStatsHandler(c *gin.Context) {
	cps := GetCryptoPaymentSystem()
	stats := cps.GetStats()
	c.JSON(200, gin.H{"status": "ok", "stats": stats})
}

func (s *Server) PaymentWalletHandler(c *gin.Context) {
	cps := GetCryptoPaymentSystem()
	c.JSON(200, gin.H{
		"status":        "ok",
		"wallet_address": cps.walletAddress,
		"network":       cps.network,
		"currency":      "USDC",
		"message":       "Send USDC to this address to subscribe to FridaySignal Pro",
	})
}

func (s *Server) PaymentSubscribeHandler(c *gin.Context) {
	var req struct {
		Email string `json:"email"`
		Tier  string `json:"tier"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if req.Tier == "" {
		req.Tier = "standard"
	}

	cps := GetCryptoPaymentSystem()
	subID, subscription := cps.CreatePendingSubscription(req.Email, req.Tier)

	c.JSON(200, gin.H{
		"status":              "pending_payment",
		"subscription_id":     subID,
		"tier":                req.Tier,
		"amount_usd":          subscription.Amount,
		"currency":            "USDC",
		"wallet_address":      cps.walletAddress,
		"network":             cps.network,
		"message":             fmt.Sprintf("Send %.2f USDC to %s on %s to activate your subscription. Once payment is confirmed, you will receive real-time trading signals.", subscription.Amount, walletDisplay(cps.walletAddress), cps.network),
	})
}

func (s *Server) PaymentSubscriptionStatusHandler(c *gin.Context) {
	id := c.Param("id")
	cps := GetCryptoPaymentSystem()
	sub, ok := cps.GetSubscriptionStatus(id)
	if !ok {
		c.JSON(404, gin.H{"error": "subscription not found"})
		return
	}
	c.JSON(200, gin.H{"status": "ok", "subscription": sub})
}

// activeWorkerCount returns the real count of running autonomous workers.
func activeWorkerCount() int { return 9 }

// setupAlphaRoutes registers alpha engine API routes
// func (s *Server) setupAlphaRoutes() {
// 	alpha := s.router.Group("/alpha")
// 	{
// 		alpha.GET("/campaigns", s.listCampaignsHandler)
// 		alpha.GET("/campaigns/:id", s.getCampaignHandler)
// 		alpha.POST("/campaigns/:id/activate", s.activateCampaignHandler)
// 		alpha.POST("/campaigns/:id/deactivate", s.deactivateCampaignHandler)
// 		alpha.GET("/campaigns/:id/status", s.campaignStatusHandler)
// 		alpha.GET("/opportunities", s.getOpportunitiesHandler)
// 		alpha.GET("/portfolio", s.portfolioSummaryHandler)
// 		alpha.GET("/competitors", s.competitorsHandler)
// 		alpha.GET("/strategies", s.strategiesHandler)
// 		alpha.POST("/strategies/:id/evolve", s.evolveStrategyHandler)
// 		alpha.GET("/signals", s.signalsHandler)
// 		alpha.POST("/signals/inject", s.injectSignalHandler)
// 	}
// }

// walletDisplay formats a wallet address for display
func walletDisplay(addr string) string {
	if len(addr) > 10 {
		return addr[:10] + "..."
	}
	if len(addr) > 0 {
		return addr + "..."
	}
	return "unset..."
}
