package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	apperrors "github.com/friday-prototype/friday-go/pkg/errors"
)

// ProviderEndpoints maps provider names to their API endpoints and env var names
var ProviderEndpoints = map[string]struct {
	Endpoint string
	EnvKey   string
}{
	"glm":      {"https://api.z.ai/api/paas/v4", "ZHIPU_API_KEY"},
	"groq":     {"https://api.groq.com/openai/v1", "GROQ_API_KEY"},
	"gemini":   {"https://generativelanguage.googleapis.com/v1/openai/", "GEMINI_API_KEY"},
	"openrouter": {"https://openrouter.ai/api/v1", "OPENROUTER_API_KEY"},
	"openai":   {"https://api.openai.com/v1", "OPENAI_API_KEY"},
	"ollama":   {"http://localhost:11434/v1", ""},
	"github":   {"https://models.inference.ai.azure.com", "GITHUB_TOKEN"},
}

// Config holds all application configuration
type Config struct {
	mu sync.RWMutex

	// Server
	Host         string
	Port         int
	GRPCPort     int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration

	// Environment
	Environment string // development, staging, production
	LogLevel    string // debug, info, warn, error
	LogFormat   string // json, text

	// API Keys (loaded from env)
	APIKeys map[string]string

	// LLM Provider Configuration
	PrimaryProvider   string
	FallbackProviders []string
	ProviderMode      string // cloud, local, hybrid
	DefaultModel      string
	MaxTokens         int
	Temperature       float64

	// HTTP Client
	HTTPTimeout     time.Duration
	MaxIdleConns    int
	MaxConnsPerHost int

	// Rate Limiting
	RateLimitEnabled bool
	RateLimitRPS     float64
	RateLimitBurst   int

	// Tracing
	TracingEnabled   bool
	TracingEndpoint  string
	TracingSampleRate float64
	TracingStdout    bool

	// Metrics
	MetricsEnabled bool
	MetricsPath    string

	// Health
	HealthPath   string
	ReadinessPath string

	// CORS
	CORSAllowedOrigins []string
	CORSAllowedMethods []string
	CORSAllowedHeaders []string

	// Database
	DatabaseDSN string
	RedisAddr   string
	RedisPassword string
	RedisDB     int

	// MT5
	MT5Login    int
	MT5Password string
	MT5Server   string
	MT5Symbol   string
	MT5Path     string

	// Trading
	TradingEnabled       bool
	InitialBalance       float64
	RiskProfile          string
	DailyLossLimit       float64
	MaxDrawdownPercent   float64
	MinConsistency       float64
	MinHoldingTimeSec    int
	MaxTradesPerHour     int
	MaxPositionSizePct   float64

	// Security
	JWTSecret       string
	JWTExpiry       time.Duration
	APIKeyHeader    string
	EnableAuth      bool

	// Cache
	CacheTTL time.Duration
	CacheMaxSize int
}

// Singleton instance
var (
	cfg  *Config
	once sync.Once
)

// Load loads configuration from environment variables with validation
func Load() (*Config, error) {
	var loadErr error
	once.Do(func() {
		cfg = &Config{}
		loadErr = cfg.loadFromEnv()
		if loadErr == nil {
			loadErr = cfg.validate()
		}
	})
	if loadErr != nil {
		return nil, loadErr
	}
	return cfg, nil
}

// MustLoad loads config and panics on error
func MustLoad() *Config {
	c, err := Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config load failed: %v\n", err)
		os.Exit(1)
	}
	return c
}

// Get returns the loaded config (must call Load first)
func Get() *Config {
	if cfg == nil {
		fmt.Fprintln(os.Stderr, "config not loaded: call config.Load() first")
		os.Exit(1)
	}
	return cfg
}

func (c *Config) loadFromEnv() error {
	// Server
	c.Host = getEnv("HOST", "0.0.0.0")
	c.Port = getEnvInt("PORT", 8000)
	c.GRPCPort = getEnvInt("GRPC_PORT", 9000)
	c.ReadTimeout = getEnvDuration("READ_TIMEOUT", 30*time.Second)
	c.WriteTimeout = getEnvDuration("WRITE_TIMEOUT", 30*time.Second)
	c.IdleTimeout = getEnvDuration("IDLE_TIMEOUT", 120*time.Second)

	// Environment
	c.Environment = getEnv("ENVIRONMENT", "development")
	c.LogLevel = getEnv("LOG_LEVEL", "info")
	c.LogFormat = getEnv("LOG_FORMAT", "json")

	// API Keys
	c.APIKeys = make(map[string]string)
	for name, ep := range ProviderEndpoints {
		if ep.EnvKey != "" {
			if key := os.Getenv(ep.EnvKey); key != "" {
				c.APIKeys[name] = key
			}
		}
	}

	// LLM Provider
	c.PrimaryProvider = getEnv("PRIMARY_PROVIDER", "zhipu")
	c.FallbackProviders = parseCSV(getEnv("FALLBACK_PROVIDERS", "groq,gemini,openrouter"))
	c.ProviderMode = getEnv("PROVIDER_MODE", "cloud")
	c.DefaultModel = getEnv("DEFAULT_MODEL", "")
	c.MaxTokens = getEnvInt("MAX_TOKENS", 4096)
	c.Temperature = getEnvFloat("TEMPERATURE", 0.7)

	// HTTP Client
	c.HTTPTimeout = getEnvDuration("HTTP_TIMEOUT", 60*time.Second)
	c.MaxIdleConns = getEnvInt("MAX_IDLE_CONNS", 100)
	c.MaxConnsPerHost = getEnvInt("MAX_CONNS_PER_HOST", 10)

	// Rate Limiting
	c.RateLimitEnabled = getEnvBool("RATE_LIMIT_ENABLED", true)
	c.RateLimitRPS = getEnvFloat("RATE_LIMIT_RPS", 100)
	c.RateLimitBurst = getEnvInt("RATE_LIMIT_BURST", 200)

	// Tracing
	c.TracingEnabled = getEnvBool("TRACING_ENABLED", false)
	c.TracingEndpoint = getEnv("TRACING_ENDPOINT", "")
	c.TracingSampleRate = getEnvFloat("TRACING_SAMPLE_RATE", 1.0)
	c.TracingStdout = getEnvBool("TRACING_STDOUT", false)

	// Metrics
	c.MetricsEnabled = getEnvBool("METRICS_ENABLED", true)
	c.MetricsPath = getEnv("METRICS_PATH", "/metrics")

	// Health
	c.HealthPath = getEnv("HEALTH_PATH", "/healthz")
	c.ReadinessPath = getEnv("READINESS_PATH", "/readyz")

	// CORS
	c.CORSAllowedOrigins = parseCSV(getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:8080"))
	c.CORSAllowedMethods = parseCSV(getEnv("CORS_ALLOWED_METHODS", "GET,POST,PUT,DELETE,OPTIONS"))
	c.CORSAllowedHeaders = parseCSV(getEnv("CORS_ALLOWED_HEADERS", "Content-Type,Authorization,X-Request-ID"))

	// Database
	c.DatabaseDSN = getEnv("DATABASE_DSN", "")
	c.RedisAddr = getEnv("REDIS_ADDR", "localhost:6379")
	c.RedisPassword = getEnv("REDIS_PASSWORD", "")
	c.RedisDB = getEnvInt("REDIS_DB", 0)

	// MT5
	c.MT5Login = getEnvInt("MT5_LOGIN", 0)
	c.MT5Password = getEnv("MT5_PASSWORD", "")
	c.MT5Server = getEnv("MT5_SERVER", "")
	c.MT5Symbol = getEnv("MT5_SYMBOL", "EURUSD")
	c.MT5Path = getEnv("MT5_PATH", "")

	// Trading
	c.TradingEnabled = getEnvBool("TRADING_ENABLED", false)
	c.InitialBalance = getEnvFloat("INITIAL_BALANCE", 5000.0)
	c.RiskProfile = getEnv("RISK_PROFILE", "micro")
	c.DailyLossLimit = getEnvFloat("DAILY_LOSS_LIMIT", 150.0)
	c.MaxDrawdownPercent = getEnvFloat("MAX_DRAWDOWN_PERCENT", 20.0)
	c.MinConsistency = getEnvFloat("MIN_CONSISTENCY", 15.0)
	c.MinHoldingTimeSec = getEnvInt("MIN_HOLDING_TIME_SEC", 60)
	c.MaxTradesPerHour = getEnvInt("MAX_TRADES_PER_HOUR", 120)
	c.MaxPositionSizePct = getEnvFloat("MAX_POSITION_SIZE_PCT", 10.0)

	// Security
	c.JWTSecret = getEnv("JWT_SECRET", "")
	c.JWTExpiry = getEnvDuration("JWT_EXPIRY", 24*time.Hour)
	c.APIKeyHeader = getEnv("API_KEY_HEADER", "X-API-Key")
	c.EnableAuth = getEnvBool("ENABLE_AUTH", false)

	// Cache
	c.CacheTTL = getEnvDuration("CACHE_TTL", 15*time.Minute)
	c.CacheMaxSize = getEnvInt("CACHE_MAX_SIZE", 10000)

	return nil
}

func (c *Config) validate() error {
	var errs []string

	// Required for production
	if c.Environment == "production" {
		if c.JWTSecret == "" {
			errs = append(errs, "JWT_SECRET is required in production")
		}
		if len(c.APIKeys) == 0 && c.ProviderMode != "local" {
			errs = append(errs, "at least one LLM API key required in production")
		}
		if c.MT5Login == 0 && c.TradingEnabled {
			errs = append(errs, "MT5_LOGIN required when trading enabled")
		}
		if c.MT5Password == "" && c.TradingEnabled {
			errs = append(errs, "MT5_PASSWORD required when trading enabled")
		}
		if c.MT5Server == "" && c.TradingEnabled {
			errs = append(errs, "MT5_SERVER required when trading enabled")
		}
		if c.DatabaseDSN == "" {
			errs = append(errs, "DATABASE_DSN required in production")
		}
	}

	// Validate provider mode
	validModes := map[string]bool{"cloud": true, "local": true, "hybrid": true}
	if !validModes[c.ProviderMode] {
		errs = append(errs, fmt.Sprintf("invalid PROVIDER_MODE: %s (must be cloud, local, or hybrid)", c.ProviderMode))
	}

	// Validate primary provider exists
	if c.PrimaryProvider != "ollama" && c.APIKeys[c.PrimaryProvider] == "" && c.ProviderMode != "local" {
		errs = append(errs, fmt.Sprintf("primary provider %s has no API key configured", c.PrimaryProvider))
	}

	// Validate numeric ranges
	if c.Port <= 0 || c.Port > 65535 {
		errs = append(errs, "PORT must be 1-65535")
	}
	if c.GRPCPort <= 0 || c.GRPCPort > 65535 {
		errs = append(errs, "GRPC_PORT must be 1-65535")
	}
	if c.RateLimitRPS <= 0 {
		errs = append(errs, "RATE_LIMIT_RPS must be positive")
	}
	if c.TracingSampleRate < 0 || c.TracingSampleRate > 1 {
		errs = append(errs, "TRACING_SAMPLE_RATE must be 0-1")
	}
	if c.MaxDrawdownPercent < 0 || c.MaxDrawdownPercent > 100 {
		errs = append(errs, "MAX_DRAWDOWN_PERCENT must be 0-100")
	}
	if c.MinConsistency < 0 || c.MinConsistency > 100 {
		errs = append(errs, "MIN_CONSISTENCY must be 0-100")
	}
	if c.MaxPositionSizePct < 0 || c.MaxPositionSizePct > 100 {
		errs = append(errs, "MAX_POSITION_SIZE_PCT must be 0-100")
	}

	if len(errs) > 0 {
		return apperrors.ErrConfigValidation.WithMeta("details", strings.Join(errs, "; "))
	}
	return nil
}

// HasAPIKey checks if an API key is configured for a provider
func (c *Config) HasAPIKey(provider string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.APIKeys[provider]
	return ok
}

// GetAPIKey returns the API key for a provider
func (c *Config) GetAPIKey(provider string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.APIKeys[provider]
}

// GetProviderEndpoint returns the endpoint for a provider
func (c *Config) GetProviderEndpoint(provider string) string {
	if ep, ok := ProviderEndpoints[provider]; ok {
		return ep.Endpoint
	}
	return ""
}

// GetActiveProviders returns providers with configured keys
func (c *Config) GetActiveProviders() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	providers := make([]string, 0, len(c.APIKeys))
	for name := range c.APIKeys {
		providers = append(providers, name)
	}
	return providers
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

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return defaultValue
}

func parseCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// String returns a sanitized string representation (no secrets)
func (c *Config) String() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return fmt.Sprintf(
		"Config{Environment:%s, Host:%s, Port:%d, GRPCPort:%d, ProviderMode:%s, PrimaryProvider:%s, TradingEnabled:%v, APIKeys:%d}",
		c.Environment, c.Host, c.Port, c.GRPCPort, c.ProviderMode, c.PrimaryProvider, c.TradingEnabled, len(c.APIKeys),
	)
}