package config

import (
	"os"
	"strconv"
	"sync"
)

// Provider endpoints matching the Python PROVIDER_ENDPOINTS
var ProviderEndpoints = map[string]struct {
	Endpoint string
	EnvKey   string
}{
	"groq":       {"https://api.groq.com/openai/v1", "GROQ_API_KEY"},
	"gemini":     {"https://generativelanguage.googleapis.com/v1/openai/", "GEMINI_API_KEY"},
	"deepseek":   {"https://api.deepseek.com", "DEEPSEEK_API_KEY"},
	"openrouter": {"https://openrouter.ai/api/v1", "OPENROUTER_API_KEY"},
	"openai":     {"https://api.openai.com/v1", "OPENAI_API_KEY"},
	"sarvam":     {"https://api.sarvam.ai", "SARVAM_API_KEY"},
	"ollama":     {"http://localhost:11434/v1", "OLLAMA_API_KEY"},
	"opencode":   {"https://opencode.ai/zen/v1", "OPENCODE_API_KEY"},
	"qwen":       {"https://dashscope-intl.aliyuncs.com/compatible-mode/v1", "DASHSCOPE_API_KEY"},
	"zhipu":      {"https://open.bigmodel.cn/api/paas/v4/", "ZHIPU_API_KEY"},
	"nvidia":     {"https://integrate.api.nvidia.com/v1", "NVIDIA_API_KEY"},
	"github":     {"https://models.inference.ai.azure.com", "GITHUB_TOKEN"},
}

// Candidate represents an LLM provider/model combination
type Candidate struct {
	Provider string
	Model    string
}

// Config holds all Friday configuration
type Config struct {
	mu sync.RWMutex

	DeployMode         string
	Host               string
	Port               int
	DevMode            bool
	APIToken           string
	EnableExecTools    bool
	Keys               map[string]string
	ProviderMode       string
	LocalModelPath     string
	LocalModelContext  int
	RoleChains         map[string][]Candidate
	MaxRetries         int
	VerifyPasses       int
	BreakerThreshold   int
	BreakerCooldown    int
	LLMCallTimeout     float64
	TurnDeadlineSec    float64
	SingleCallMode     bool
	TTSGenerateAsync   bool
	TTSPaidEnabled     bool
	PrimaryChatProvider string
	OllamaModel        string
	OllamaModelFast    string
	OllamaVisionModel  string
	EnableAnticipation bool
	EnableUserMemory   bool
	EnableSelfCheck    bool
	LocalCacheTTL      float64
	ResearchDir        string
	ResearchMaxSteps   int
	ResearchMaxFixes   int
	ResearchBrowser    bool
	ExecuteOnHost      bool
	ConfirmDestructive bool
	STTProvider        string
	TTSEngine          string
	TTSVoiceEN         string
	TTSVoiceEdge       string
	TTSPitchEdge       string
	AllowGTTSEVoice    bool
	VoiceNatural       bool
	LocalVisionEnabled bool
	ScreenWatch        bool
	ScreenWatchInterval int
	ScreenWatchFast    bool
	ScreenWatchFastInterval int
	EyeGuideOnly       bool
	ProactiveAct       bool
	WakeWord           string
	GreetingText       string
	ListeningTimeout   int
	SarvamSTTModel     string
	SarvamTTSModel     string
	DataDir            string
	WifeAgentVoice     string
	WifeAgentName      string
	WifeAgentEnabled   bool
}

var config *Config
var once sync.Once

// GetConfig returns the singleton Config instance
func GetConfig() *Config {
	once.Do(func() {
		config = &Config{
			DeployMode:          getEnv("DEPLOY_MODE", "local"),
			Host:                getEnv("HOST", "0.0.0.0"),
			Port:                getEnvInt("PORT", 8000),
			DevMode:             getEnvBool("DEV_MODE", true),
			APIToken:            getEnv("FRIDAY_TOKEN", ""),
			EnableExecTools:     getEnvBool("ENABLE_EXEC_TOOLS", true),
			Keys:                make(map[string]string),
			ProviderMode:        getEnv("PROVIDER_MODE", "cloud"),
			LocalModelPath:      getEnv("LOCAL_MODEL_PATH", ""),
			LocalModelContext:   getEnvInt("LOCAL_MODEL_CONTEXT", 8192),
			MaxRetries:          getEnvInt("MAX_RETRIES", 5),
			VerifyPasses:        getEnvInt("VERIFY_PASSES", 1),
			BreakerThreshold:    getEnvInt("BREAKER_THRESHOLD", 5),
			BreakerCooldown:     getEnvInt("BREAKER_COOLDOWN", 60),
			LLMCallTimeout:      getEnvFloat("LLM_CALL_TIMEOUT", 120),
			TurnDeadlineSec:     getEnvFloat("TURN_DEADLINE_SECONDS", 300),
			SingleCallMode:      getEnvBool("SINGLE_CALL_MODE", true),
			TTSGenerateAsync:    getEnvBool("TTS_GENERATE_ASYNC", true),
			TTSPaidEnabled:      getEnvBool("TTS_PAID_ENABLED", false),
			PrimaryChatProvider: getEnv("PRIMARY_CHAT_PROVIDER", "deepseek"),
			OllamaModel:         getEnv("OLLAMA_MODEL", "qwen3.5:9b"),
			OllamaModelFast:     getEnv("OLLAMA_MODEL_FAST", "qwen2.5:1.5b"),
			OllamaVisionModel:   getEnv("OLLAMA_VISION_MODEL", "moondream:latest"),
			EnableAnticipation:  getEnvBool("ENABLE_ANTICIPATION", true),
			EnableUserMemory:    getEnvBool("ENABLE_USER_MEMORY", true),
			EnableSelfCheck:     getEnvBool("ENABLE_SELFCHECK", false),
			LocalCacheTTL:       getEnvFloat("LOCAL_CACHE_TTL", 600),
			ResearchDir:         getEnv("RESEARCH_DIR", ""),
			ResearchMaxSteps:    getEnvInt("RESEARCH_MAX_STEPS", 25),
			ResearchMaxFixes:    getEnvInt("RESEARCH_MAX_FIXES", 6),
			ResearchBrowser:     getEnvBool("RESEARCH_BROWSER", true),
			ExecuteOnHost:       getEnvBool("EXECUTE_ON_HOST", false),
			ConfirmDestructive:  getEnvBool("CONFIRM_DESTRUCTIVE", false),
			STTProvider:         getEnv("STT_PROVIDER", "sarvam"),
			TTSEngine:           getEnv("TTS_ENGINE", "edge"),
			TTSVoiceEN:          getEnv("TTS_VOICE_EN", "en-IN-ShaanNeural"),
			TTSVoiceEdge:        getEnv("TTS_VOICE_EDGE", "en-IN-NeerjaNeural"),
			TTSPitchEdge:        getEnv("TTS_PITCH_EDGE", "+0Hz"),
			AllowGTTSEVoice:     getEnvBool("ALLOW_GTTS_VOICE", false),
			VoiceNatural:        getEnvBool("VOICE_NATURAL", true),
			LocalVisionEnabled:  getEnvBool("LOCAL_VISION_ENABLED", true),
			ScreenWatch:         getEnvBool("SCREEN_WATCH", false),
			ScreenWatchInterval: getEnvInt("SCREEN_WATCH_INTERVAL", 20),
			ScreenWatchFast:     getEnvBool("SCREEN_WATCH_FAST", false),
			ScreenWatchFastInterval: getEnvInt("SCREEN_WATCH_FAST_INTERVAL", 1),
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

		// Load API keys
		for name, ep := range ProviderEndpoints {
			if k := getEnv(ep.EnvKey, ""); k != "" {
				config.Keys[name] = k
			}
		}

		// Initialize role chains
		config.initRoleChains()
	})
	return config
}

func (c *Config) initRoleChains() {
	// Get model names
	mGroq := getEnv("GROQ_MODEL", "llama-3.3-70b-versatile")
	mGemini := getEnv("GEMINI_MODEL", "gemini-2.0-flash")
	mDeep := getEnv("DEEPSEEK_MODEL", "deepseek-chat")
	mDeepReason := getEnv("DEEPSEEK_REASONER_MODEL", "deepseek-reasoner")
	mOr := getEnv("OPENROUTER_MODEL", "meta-llama/llama-3.3-70b-instruct:free")
	mOpenai := getEnv("OPENAI_MODEL", "gpt-4o-mini")
	mQwen := getEnv("QWEN_MODEL", "qwen-plus")
	mZhipu := getEnv("ZHIPU_MODEL", "glm-5.2")
	mOpencode := getEnv("OPENCODE_MODEL", "deepseek-v4-flash-free")
	mNvidia := getEnv("NVIDIA_MODEL", "meta/llama-3.3-70b-instruct")
	mGithub := getEnv("GITHUB_MODEL", "gpt-4o-mini")

	modelMap := map[string]string{
		"groq": mGroq, "gemini": mGemini, "deepseek": mDeep,
		"openrouter": mOr, "openai": mOpenai,
		"qwen": mQwen, "zhipu": mZhipu, "opencode": mOpencode,
		"nvidia": mNvidia, "github": mGithub,
	}
	_ = modelMap // kept for future model lookups by provider name

	// Define candidates
	chatCandidates := []Candidate{
		{Provider: "opencode", Model: mOpencode},
		{Provider: "groq", Model: mGroq},
		{Provider: "gemini", Model: mGemini},
		{Provider: "openrouter", Model: mOr},
		{Provider: "deepseek", Model: mDeep},
		{Provider: "nvidia", Model: mNvidia},
		{Provider: "github", Model: mGithub},
		{Provider: "zhipu", Model: mZhipu},
		{Provider: "qwen", Model: mQwen},
	}

	reasonCandidates := []Candidate{
		{Provider: "opencode", Model: mOpencode},
		{Provider: "groq", Model: mGroq},
		{Provider: "gemini", Model: mGemini},
		{Provider: "openrouter", Model: mOr},
		{Provider: "deepseek", Model: mDeepReason},
		{Provider: "deepseek", Model: mDeep},
		{Provider: "nvidia", Model: mNvidia},
		{Provider: "github", Model: mGithub},
		{Provider: "zhipu", Model: mZhipu},
		{Provider: "qwen", Model: mQwen},
	}

	c.RoleChains = map[string][]Candidate{
		"companion":  chatCandidates,
		"reasoner":   reasonCandidates,
		"coder":      reasonCandidates,
		"researcher": reasonCandidates,
		"judge":      chatCandidates,
		"verifier":   chatCandidates,
		"router":     chatCandidates,
	}
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

func getEnvFloat(key string, defaultValue float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
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

// GetEndpoint returns the endpoint URL for a provider
func (c *Config) GetEndpoint(provider string) string {
	if ep, ok := ProviderEndpoints[provider]; ok {
		return ep.Endpoint
	}
	return ""
}

// HasKey checks if a provider has an API key configured
func (c *Config) HasKey(provider string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if provider == "ollama" && c.ProviderMode == "local" {
		return true
	}
	if provider == "opencode" {
		return true // no API key needed
	}
	_, ok := c.Keys[provider]
	return ok
}

// HasAnyKey checks if any provider key is configured
func (c *Config) HasAnyKey() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.ProviderMode == "local" {
		return true
	}
	if c.HasKey("opencode") {
		return true
	}
	return len(c.Keys) > 0
}

// LiveChain returns the active provider chain for a role
func (c *Config) LiveChain(role string) []Candidate {
	c.mu.RLock()
	defer c.mu.RUnlock()

	chain := c.RoleChains[role]
	if len(chain) == 0 {
		chain = c.RoleChains["companion"]
	}

	// Hybrid mode: always try local Ollama first, then cloud providers
	candidates := make([]Candidate, 0, len(chain))
	for _, cand := range chain {
		if cand.Provider == "ollama" {
			candidates = append(candidates, cand)
		} else if c.HasKey(cand.Provider) {
			candidates = append(candidates, cand)
		}
	}
	return candidates
}

// ActiveProviders returns list of configured providers
func (c *Config) ActiveProviders() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	providers := make([]string, 0, len(c.Keys))
	for name := range c.Keys {
		providers = append(providers, name)
	}
	return providers
}