package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	once sync.Once

	// HTTP metrics
	HTTPRequestsTotal *prometheus.CounterVec
	HTTPRequestDuration *prometheus.HistogramVec

	// LLM metrics
	LLMRequestsTotal *prometheus.CounterVec
	LLMRequestDuration *prometheus.HistogramVec
	LLMTokensUsed *prometheus.CounterVec

	// Trading metrics
	TradesTotal *prometheus.CounterVec
	TradeDuration *prometheus.HistogramVec
	PositionPNL *prometheus.GaugeVec
	AccountBalance *prometheus.GaugeVec
	DailyPNL *prometheus.GaugeVec

	// Safety metrics
	SafetyChecksTotal *prometheus.CounterVec

	registry = prometheus.NewRegistry()
)

// Init initializes all metrics
func Init() {
	once.Do(func() {
		// HTTP metrics
		HTTPRequestsTotal = registerCounterVec("friday_http_requests_total", "Total HTTP requests", []string{"method", "path", "status"})
		HTTPRequestDuration = registerHistogramVec("friday_http_request_duration_seconds", "HTTP request latency", []string{"method", "path"}, []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10})

		// LLM metrics
		LLMRequestsTotal = registerCounterVec("friday_llm_requests_total", "Total LLM requests", []string{"provider", "model", "status"})
		LLMRequestDuration = registerHistogramVec("friday_llm_request_duration_seconds", "LLM request latency", []string{"provider", "model"}, []float64{0.1, 0.5, 1, 2, 5, 10})
		LLMTokensUsed = registerCounterVec("friday_llm_tokens_used_total", "Tokens used", []string{"provider", "model", "type"})

		// Trading metrics
		TradesTotal = registerCounterVec("friday_trades_total", "Total trades", []string{"symbol", "direction", "status"})
		TradeDuration = registerHistogramVec("friday_trade_duration_seconds", "Trade duration", []string{"broker"}, []float64{0.001, 0.01, 0.05, 0.1, 0.5, 1})
		PositionPNL = registerGaugeVec("friday_position_pnl", "Position PnL", []string{"symbol"})
		AccountBalance = registerGaugeVec("friday_account_balance", "Account balance", []string{})
		DailyPNL = registerGaugeVec("friday_daily_pnl", "Daily PnL", []string{})

		// Safety metrics
		SafetyChecksTotal = registerCounterVec("friday_safety_checks_total", "Safety checks", []string{"check", "result"})

		// Register collectors
		registry.MustRegister(prometheus.NewGoCollector())
		registry.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	})
}

// Registry returns the prometheus registry
func Registry() *prometheus.Registry {
	Init()
	return registry
}

// Helper registration functions
func registerCounterVec(name, help string, labels []string) *prometheus.CounterVec {
	cv := prometheus.NewCounterVec(prometheus.CounterOpts{Name: name, Help: help}, labels)
	registry.MustRegister(cv)
	return cv
}

func registerHistogramVec(name, help string, labels []string, buckets []float64) *prometheus.HistogramVec {
	hv := prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: name, Help: help, Buckets: buckets}, labels)
	registry.MustRegister(hv)
	return hv
}

func registerGaugeVec(name, help string, labels []string) *prometheus.GaugeVec {
	gv := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: name, Help: help}, labels)
	registry.MustRegister(gv)
	return gv
}

// Recording helpers
func RecordHTTPRequest(method, path, status string, duration float64) {
	Init()
	HTTPRequestsTotal.WithLabelValues(method, path, status).Inc()
	HTTPRequestDuration.WithLabelValues(method, path).Observe(duration)
}

func RecordLLMRequest(provider, model, status string, duration float64, promptTokens, completionTokens int) {
	Init()
	LLMRequestsTotal.WithLabelValues(provider, model, status).Inc()
	LLMRequestDuration.WithLabelValues(provider, model).Observe(duration)
	LLMTokensUsed.WithLabelValues(provider, model, "prompt").Add(float64(promptTokens))
	LLMTokensUsed.WithLabelValues(provider, model, "completion").Add(float64(completionTokens))
}

func RecordTrade(symbol, direction, status string, duration float64) {
	Init()
	TradesTotal.WithLabelValues(symbol, direction, status).Inc()
	TradeDuration.WithLabelValues("mt5").Observe(duration)
}

func RecordSafetyCheck(check, result string) {
	Init()
	SafetyChecksTotal.WithLabelValues(check, result).Inc()
}

func SetPositionPNL(symbol string, pnl float64) {
	Init()
	PositionPNL.WithLabelValues(symbol).Set(pnl)
}

func SetAccountBalance(balance float64) {
	Init()
	AccountBalance.WithLabelValues().Set(balance)
}

func SetDailyPNL(pnl float64) {
	Init()
	DailyPNL.WithLabelValues().Set(pnl)
}