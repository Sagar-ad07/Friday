package friday

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config holds all Friday configuration - SINGLE SOURCE OF TRUTH
type Config struct {
	mu sync.RWMutex

	// Server
	DeployMode      string
	Host            string
	Port            int
	DevMode         bool
	APIToken        string
	EnableExecTools bool

	// API keys by provider
	Keys         map[string]string
	ProviderMode string

	// Resilience
	MaxRetries        int
	VerifyPasses      int
	BreakerThreshold  int
	BreakerCooldown   time.Duration
	LLMCallTimeout    time.Duration
	TurnDeadlineSec   time.Duration
	SingleCallMode    bool

	// Voice
	STTProvider      string
	TTSEngine        string
	TTSVoiceEN       string
	TTSVoiceEdge     string
	TTSPitchEdge     string
	AllowGTTSEVoice  bool
	VoiceNatural     bool

	// Local models
	LocalModelPath    string
	LocalModelContext int
	OllamaModel       string

	// Smooth Voice
	SmoothVoice      *SmoothVoiceSystem
	RevolutionTrading *RevolutionTradingSystem
	OllamaModelFast   string
	OllamaVisionModel string

	// Features
	EnableAnticipation bool
	EnableUserMemory   bool
	EnableSelfCheck    bool
	LocalCacheTTL      time.Duration
	ResearchDir        string
	ResearchMaxSteps   int
	ResearchMaxFixes   int
	ResearchBrowser    bool
	ExecuteOnHost      bool
	ConfirmDestructive bool

	// Screen watch
	LocalVisionEnabled    bool
	ScreenWatch           bool
	ScreenWatchInterval   time.Duration
	ScreenWatchFast       bool
	ScreenWatchFastInterval time.Duration
	EyeGuideOnly          bool
	ProactiveAct          bool

	// Wake word
	WakeWord          string
	GreetingText      string
	ListeningTimeout  int

	// Sarvam
	SarvamSTTModel string
	SarvamTTSModel string

	// Data
	DataDir string

	// Wife agent
	WifeAgentVoice string
	WifeAgentName  string
 	WifeAgentEnabled bool

 	// Service URLs (accessed via getters)
 	LLMBridgeURL      string
 	LLMBridgePort     int
 	TradingEngineURL  string
 	TradingEnginePort int
 	UpgradeInterval   time.Duration

 	// Cache
 	CacheCapacity    int
 	CacheTTL         time.Duration
 	MaxConversations int

	// Testnet Campaign Automation
	CampaignAutoDeploy    bool
	CampaignAutoTransact  bool
	CampaignAutoClaim     bool
	CampaignWallets       []string
	CampaignRPCs          map[string]string
	CampaignDockerImages  map[string]string
	CampaignCheckInterval time.Duration

	// Resilience
	RateLimitPerMin    int
	CircuitThreshold   int
	CircuitCooldown    time.Duration
	MaxConcurrent      int
	MaxBodyBytes       int64
	DirectEndpoint     string
	DirectModel        string

	// Breach chain (vision proxy — Gemini handles image tasks)
	BreachChain Chain
	GLMEndpoint   string
	GLMModel      string

	// Provider endpoints (internal)
	providerEndpoints map[Provider]ProviderEndpoint
}

type ProviderEndpoint struct {
	Endpoint string
	EnvKey   string
}

var (
	cfg     *Config
	cfgOnce sync.Once
)

// DefaultProviderEndpoints returns the canonical provider endpoints
func DefaultProviderEndpoints() map[Provider]ProviderEndpoint {
	return map[Provider]ProviderEndpoint{
		ProviderGLM:        {Endpoint: "https://api.z.ai/api/paas/v4", EnvKey: "ZHIPU_API_KEY"},
		ProviderOpenRouter: {Endpoint: "https://openrouter.ai/api/v1", EnvKey: "OPENROUTER_API_KEY"},
		ProviderGemini:     {Endpoint: "https://generativelanguage.googleapis.com/v1/openai/", EnvKey: "GEMINI_API_KEY"},
		ProviderOpencode:   {Endpoint: "https://opencode.ai/zen/v1", EnvKey: "OPENCODE_API_KEY"},
		ProviderOllama:     {Endpoint: "http://localhost:11434/v1", EnvKey: "OLLAMA_API_KEY"},
		ProviderGroq:       {Endpoint: "https://api.groq.com/openai/v1", EnvKey: "GROQ_API_KEY"},
	}
}

// GetConfig returns the singleton Config instance
func GetConfig() *Config {
	cfgOnce.Do(func() {
		cfg = loadConfig()
	})
	return cfg
}

func loadConfig() *Config {
	// Auto-load .env file from project root (or its parent for desktop builds)
	loadEnvFile(filepath.Join(ProjectRoot, ".env"))
	loadEnvFile(filepath.Join(filepath.Dir(ProjectRoot), ".env"))

	c := &Config{
		DeployMode:          getEnv("DEPLOY_MODE", "local"),
		Host:                getEnv("HOST", "0.0.0.0"),
		Port:                getEnvInt("PORT", 8000),
		DevMode:             getEnvBool("DEV_MODE", true),
		APIToken:            getEnv("FRIDAY_TOKEN", ""),
		EnableExecTools:     getEnvBool("ENABLE_EXEC_TOOLS", true),
		ProviderMode:        getEnv("PROVIDER_MODE", "cloud"),
		MaxRetries:          getEnvInt("MAX_RETRIES", 5),
		VerifyPasses:        getEnvInt("VERIFY_PASSES", 1),
		BreakerThreshold:    getEnvInt("BREAKER_THRESHOLD", 5),
		BreakerCooldown:     getEnvDuration("BREAKER_COOLDOWN", 60*time.Second),
		LLMCallTimeout:      getEnvDuration("LLM_CALL_TIMEOUT", 120*time.Second),
		TurnDeadlineSec:     getEnvDuration("TURN_DEADLINE_SECONDS", 300*time.Second),
		SingleCallMode:      getEnvBool("SINGLE_CALL_MODE", true),
		STTProvider:         getEnv("STT_PROVIDER", "sarvam"),
		TTSEngine:           getEnv("TTS_ENGINE", "edge"),
		TTSVoiceEN:          getEnv("TTS_VOICE_EN", "en-IN-ShaanNeural"),
		TTSVoiceEdge:        getEnv("TTS_VOICE_EDGE", "en-IN-NeerjaNeural"),
		TTSPitchEdge:        getEnv("TTS_PITCH_EDGE", "+0Hz"),
		AllowGTTSEVoice:     getEnvBool("ALLOW_GTTS_VOICE", false),
		VoiceNatural:        getEnvBool("VOICE_NATURAL", true),
		LocalModelPath:      getEnv("LOCAL_MODEL_PATH", ""),
		LocalModelContext:   getEnvInt("LOCAL_MODEL_CONTEXT", 8192),
		OllamaModel:         getEnv("OLLAMA_MODEL", "qwen2.5:9b"),
		OllamaModelFast:     getEnv("OLLAMA_MODEL_FAST", "qwen2.5:1.5b"),
		OllamaVisionModel:   getEnv("OLLAMA_VISION_MODEL", "moondream:latest"),
		EnableAnticipation:  getEnvBool("ENABLE_ANTICIPATION", true),
		EnableUserMemory:    getEnvBool("ENABLE_USER_MEMORY", true),
		EnableSelfCheck:     getEnvBool("ENABLE_SELFCHECK", false),
		LocalCacheTTL:       getEnvDuration("LOCAL_CACHE_TTL", 600*time.Second),
		ResearchDir:         getEnv("RESEARCH_DIR", ""),
		ResearchMaxSteps:    getEnvInt("RESEARCH_MAX_STEPS", 25),
		ResearchMaxFixes:    getEnvInt("RESEARCH_MAX_FIXES", 6),
		ResearchBrowser:     getEnvBool("RESEARCH_BROWSER", true),
		ExecuteOnHost:       getEnvBool("EXECUTE_ON_HOST", false),
		ConfirmDestructive:  getEnvBool("CONFIRM_DESTRUCTIVE", false),
		LocalVisionEnabled:  getEnvBool("LOCAL_VISION_ENABLED", true),
		ScreenWatch:         getEnvBool("SCREEN_WATCH", false),
		ScreenWatchInterval: getEnvDuration("SCREEN_WATCH_INTERVAL", 20*time.Second),
		ScreenWatchFast:     getEnvBool("SCREEN_WATCH_FAST", false),
		ScreenWatchFastInterval: getEnvDuration("SCREEN_WATCH_FAST_INTERVAL", 1*time.Second),
		EyeGuideOnly:        getEnvBool("EYE_GUID_ONLY", true),
		ProactiveAct:        getEnvBool("PROACTIVE_ACT", false),
		WakeWord:            getEnv("WAKE_WORD", "friday"),
		GreetingText:        getEnv("GREETING_TEXT", "At your service, sir"),
		ListeningTimeout:    getEnvInt("LISTENING_TIMEOUT", 8),
		SarvamSTTModel:      getEnv("SARVAM_STT_MODEL", "saaras:v3"),
		SarvamTTSModel:      getEnv("SARVAM_TTS_MODEL", "bulbul:v3"),
		DataDir:             getEnv("MEMORY_DIR", ""),
		WifeAgentVoice:      getEnv("WIFE_AGENT_VOICE", "en-IN-NeerjaNeural"),
		WifeAgentName:       getEnv("WIFE_AGENT_NAME", "Assistant"),
		WifeAgentEnabled:    getEnvBool("WIFE_AGENT_ENABLED", false),
	}

	c.providerEndpoints = DefaultProviderEndpoints()
	c.Keys = make(map[string]string)

	// Load API keys from environment
	for provider, ep := range c.providerEndpoints {
		if key := getEnv(ep.EnvKey, ""); key != "" {
			c.Keys[string(provider)] = key
		}
	}



	return c
}

func (c *Config) initRoleChains() {
	// No-op: role chains removed in simplified architecture
}



func (c *Config) HasKey(provider Provider) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if provider == ProviderOllama && c.ProviderMode == "local" {
		return true
	}
	if provider == ProviderOpencode {
		return true // no API key needed
	}
	_, ok := c.Keys[string(provider)]
	return ok
}

func (c *Config) HasAnyKey() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.ProviderMode == "local" {
		return true
	}
	if c.HasKey(ProviderOpencode) {
		return true
	}
	return len(c.Keys) > 0
}

func (c *Config) Endpoint(provider Provider) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if ep, ok := c.providerEndpoints[provider]; ok {
		return ep.Endpoint
	}
	return ""
}

// Helper functions
func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		if sec, err := strconv.Atoi(v); err == nil {
			return time.Duration(sec) * time.Second
		}
	}
	return defaultValue
}

// DeepSeekKey returns the DeepSeek API key if configured
func (c *Config) DeepSeekKey() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Keys["deepseek"]
}

// OpenRouterKey returns the OpenRouter API key if configured
func (c *Config) OpenRouterKey() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Keys["openrouter"]
}

// GeminiKey returns the Gemini API key if configured
func (c *Config) GeminiKey() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Keys["gemini"]
}

// GetLLMBridgeURL returns the LLM Bridge URL
func (c *Config) GetLLMBridgeURL() string {
	return getEnv("LLM_BRIDGE_URL", "http://localhost:8000")
}

// GetTradingEngineURL returns the Trading Engine URL
func (c *Config) GetTradingEngineURL() string {
	return getEnv("TRADING_ENGINE_URL", "http://localhost:8001")
}

// GetUpgradeInterval returns the upgrader check interval
func (c *Config) GetUpgradeInterval() time.Duration {
	return getEnvDuration("UPGRADE_INTERVAL", 6*time.Hour)
}

// LoadConfig is alias for GetConfig for backwards compatibility
func LoadConfig() *Config {
	return GetConfig()
}

func (c *Config) GetLLMBridgePort() int {
	return getEnvInt("LLM_BRIDGE_PORT", 9001)
}

func (c *Config) GetTradingEnginePort() int {
	return getEnvInt("TRADING_ENGINE_PORT", 8001)
}

func (c *Config) GetCacheCapacity() int {
	return getEnvInt("CACHE_CAPACITY", 2000)
}

func (c *Config) GetCacheTTL() time.Duration {
	return getEnvDuration("CACHE_TTL", 30*time.Minute)
}

func (c *Config) GetMaxConversations() int {
	return getEnvInt("MAX_CONVERSATIONS", 500)
}

func (c *Config) GetRateLimitPerMin() int {
	return getEnvInt("RATE_LIMIT_PER_MIN", 9999)
}

func (c *Config) GetCircuitThreshold() int {
	return getEnvInt("CIRCUIT_THRESHOLD", 3)
}

func (c *Config) GetCircuitCooldown() time.Duration {
	return getEnvDuration("CIRCUIT_COOLDOWN", 30*time.Second)
}

func (c *Config) GetMaxConcurrent() int {
	return getEnvInt("MAX_CONCURRENT", 50)
}

func (c *Config) GetMaxBodyBytes() int64 {
	return int64(getEnvInt("MAX_BODY_BYTES", 1048576))
}

func (c *Config) GetDirectEndpoint() string {
	return getEnv("DIRECT_ENDPOINT", "https://opencode.ai/zen/v1")
}

func (c *Config) GetDirectModel() string {
	return getEnv("DIRECT_MODEL", "deepseek-v4-flash-free")
}

func (c *Config) GetDeepSeekEndpoint() string {
	return getEnv("DEEPSEEK_ENDPOINT", "https://api.deepseek.com/v1")
}

func (c *Config) GetDeepSeekModel() string {
	return getEnv("DEEPSEEK_MODEL", "deepseek-chat")
}

// loadEnvFile reads a .env file and sets environment variables.
// Lines must be KEY=VALUE. Comments start with #. Empty lines ignored.
func loadEnvFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return // .env file is optional
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if key == "" {
			continue
		}
		// Only set if not already set in the actual environment
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}