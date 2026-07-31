package errors

import (
	"errors"
	"fmt"
)

// Sentinel errors for common cases
var (
	// Config errors
	ErrConfigNotFound     = New("CONFIG_NOT_FOUND", "configuration not found")
	ErrConfigInvalid      = New("CONFIG_INVALID", "invalid configuration")
	ErrConfigMissingKey   = New("CONFIG_MISSING_KEY", "required configuration key missing")
	ErrConfigValidation   = New("CONFIG_VALIDATION_FAILED", "configuration validation failed")

	// LLM errors
	ErrLLMProviderUnavailable = New("LLM_PROVIDER_UNAVAILABLE", "LLM provider unavailable")
	ErrLLMModelNotFound       = New("LLM_MODEL_NOT_FOUND", "model not found")
	ErrLLMRateLimited         = New("LLM_RATE_LIMITED", "rate limited by provider")
	ErrLLMTimeout             = New("LLM_TIMEOUT", "LLM request timed out")
	ErrLLMContextTooLarge     = New("LLM_CONTEXT_TOO_LARGE", "context exceeds model limit")
	ErrLLMInvalidResponse     = New("LLM_INVALID_RESPONSE", "invalid response from LLM")
	ErrLLMAllProvidersFailed  = New("LLM_ALL_PROVIDERS_FAILED", "all LLM providers failed")
	ErrLLMInvalidAPIKey       = New("LLM_INVALID_API_KEY", "invalid API key")
	ErrLLMQuotaExceeded       = New("LLM_QUOTA_EXCEEDED", "provider quota exceeded")

	// Memory errors
	ErrMemoryNotFound       = New("MEMORY_NOT_FOUND", "memory entry not found")
	ErrMemoryStoreFailed    = New("MEMORY_STORE_FAILED", "failed to store memory")
	ErrMemoryRetrieveFailed = New("MEMORY_RETRIEVE_FAILED", "failed to retrieve memory")
	ErrMemoryEmbeddingFailed = New("MEMORY_EMBEDDING_FAILED", "failed to generate embedding")
	ErrMemoryIndexFailed    = New("MEMORY_INDEX_FAILED", "failed to index memory")

	// Trading errors
	ErrTradingNotRunning     = New("TRADING_NOT_RUNNING", "trading bot not running")
	ErrTradingAlreadyRunning = New("TRADING_ALREADY_RUNNING", "trading bot already running")
	ErrInvalidSymbol         = New("INVALID_SYMBOL", "invalid trading symbol")
	ErrInvalidOrder          = New("INVALID_ORDER", "invalid order parameters")
	ErrInvalidOrderType      = New("INVALID_ORDER_TYPE", "invalid order type")
	ErrInvalidOrderStatus    = New("INVALID_ORDER_STATUS", "invalid order status")
	ErrInsufficientFunds     = New("INSUFFICIENT_FUNDS", "insufficient account balance")
	ErrPositionNotFound      = New("POSITION_NOT_FOUND", "position not found")
	ErrPositionAlreadyClosed = New("POSITION_ALREADY_CLOSED", "position already closed")
	ErrMT5NotConnected       = New("MT5_NOT_CONNECTED", "MT5 terminal not connected")
	ErrMT5OrderFailed        = New("MT5_ORDER_FAILED", "MT5 order execution failed")
	ErrMT5SymbolNotFound     = New("MT5_SYMBOL_NOT_FOUND", "symbol not found in MT5")
	ErrSafetyCheckFailed     = New("SAFETY_CHECK_FAILED", "safety check failed")
	ErrDailyLossLimit        = New("DAILY_LOSS_LIMIT", "daily loss limit exceeded")
	ErrMaxDrawdownExceeded   = New("MAX_DRAWDOWN_EXCEEDED", "maximum drawdown exceeded")
	ErrMinConsistency        = New("MIN_CONSISTENCY", "minimum consistency requirement not met")
	ErrMinHoldingTime        = New("MIN_HOLDING_TIME", "minimum holding time not met")
	ErrMaxTradesPerHour      = New("MAX_TRADES_PER_HOUR", "maximum trades per hour exceeded")
	ErrMaxSpread             = New("MAX_SPREAD", "maximum spread exceeded")
	ErrInsufficientMargin    = New("INSUFFICIENT_MARGIN", "insufficient margin for position")
	ErrAccountNotFound       = New("ACCOUNT_NOT_FOUND", "trading account not found")

	// Agent errors
	ErrAgentNotFound       = New("AGENT_NOT_FOUND", "agent not found")
	ErrAgentExecutionFailed = New("AGENT_EXECUTION_FAILED", "agent execution failed")
	ErrAgentTimeout        = New("AGENT_TIMEOUT", "agent execution timed out")
	ErrAgentBusy           = New("AGENT_BUSY", "agent is busy")
	ErrToolNotFound        = New("TOOL_NOT_FOUND", "tool not found")
	ErrToolExecutionFailed = New("TOOL_EXECUTION_FAILED", "tool execution failed")
	ErrToolInvalidArgs     = New("TOOL_INVALID_ARGS", "invalid tool arguments")
	ErrToolTimeout         = New("TOOL_TIMEOUT", "tool execution timed out")
	ErrHandoffFailed       = New("HANDOFF_FAILED", "agent handoff failed")

	// HTTP/Transport errors
	ErrBadRequest          = New("BAD_REQUEST", "invalid request")
	ErrUnauthorized        = New("UNAUTHORIZED", "unauthorized")
	ErrForbidden           = New("FORBIDDEN", "forbidden")
	ErrNotFound            = New("NOT_FOUND", "resource not found")
	ErrInternalServer      = New("INTERNAL_SERVER_ERROR", "internal server error")
	ErrServiceUnavailable  = New("SERVICE_UNAVAILABLE", "service unavailable")
	ErrGatewayTimeout      = New("GATEWAY_TIMEOUT", "gateway timeout")
	ErrRequestTimeout      = New("REQUEST_TIMEOUT", "request timeout")
	ErrTooManyRequests     = New("TOO_MANY_REQUESTS", "too many requests")

	// Validation errors
	ErrValidationFailed    = New("VALIDATION_FAILED", "validation failed")
	ErrInvalidInput        = New("INVALID_INPUT", "invalid input")
	ErrMissingField        = New("MISSING_FIELD", "required field missing")
	ErrInvalidArgument     = New("INVALID_ARGUMENT", "invalid argument")

	// Database/Storage errors
	ErrDBConnection        = New("DB_CONNECTION_FAILED", "database connection failed")
	ErrDBQuery             = New("DB_QUERY_FAILED", "database query failed")
	ErrDBTransaction       = New("DB_TRANSACTION_FAILED", "database transaction failed")
	ErrRecordNotFound      = New("RECORD_NOT_FOUND", "record not found")
	ErrDuplicateRecord     = New("DUPLICATE_RECORD", "duplicate record")
	ErrConstraintViolation = New("CONSTRAINT_VIOLATION", "database constraint violation")

	// Circuit breaker
	ErrCircuitOpen         = New("CIRCUIT_OPEN", "circuit breaker is open")
	ErrCircuitHalfOpen     = New("CIRCUIT_HALF_OPEN", "circuit breaker is half-open")

	// Context errors
	ErrContextCanceled     = New("CONTEXT_CANCELED", "context canceled")
	ErrContextDeadline     = New("CONTEXT_DEADLINE_EXCEEDED", "context deadline exceeded")
)

// AppError represents an application error with code and message
type AppError struct {
	Code    string
	Message string
	Err     error
	Meta    map[string]any
}

// New creates a new AppError
func New(code, message string) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
	}
}

// Error implements the error interface
func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap returns the wrapped error
func (e *AppError) Unwrap() error {
	return e.Err
}

// WithError wraps an existing error
func (e *AppError) WithError(err error) *AppError {
	e.Err = err
	return e
}

// WithMeta adds metadata
func (e *AppError) WithMeta(key string, value any) *AppError {
	if e.Meta == nil {
		e.Meta = make(map[string]any)
	}
	e.Meta[key] = value
	return e
}

// WithMetaMap adds multiple metadata
func (e *AppError) WithMetaMap(meta map[string]any) *AppError {
	if e.Meta == nil {
		e.Meta = make(map[string]any)
	}
	for k, v := range meta {
		e.Meta[k] = v
	}
	return e
}

// Is checks if the error matches a sentinel error
func (e *AppError) Is(target error) bool {
	var appErr *AppError
	if errors.As(target, &appErr) {
		return e.Code == appErr.Code
	}
	return false
}

// Wrap wraps an error with additional context
func Wrap(err error, code, message string) error {
	if err == nil {
		return nil
	}
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.WithError(fmt.Errorf("%s: %w", message, err))
	}
	return New(code, message).WithError(err)
}

// Wrapf wraps an error with formatted message
func Wrapf(err error, code, format string, args ...any) error {
	return Wrap(err, code, fmt.Sprintf(format, args...))
}

// AsAppError converts an error to AppError if possible
func AsAppError(err error) (*AppError, bool) {
	var appErr *AppError
	ok := errors.As(err, &appErr)
	return appErr, ok
}

// IsCode checks if error has a specific code
func IsCode(err error, code string) bool {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Code == code
	}
	return false
}

// IsAnyCode checks if error matches any of the given codes
func IsAnyCode(err error, codes ...string) bool {
	var appErr *AppError
	if errors.As(err, &appErr) {
		for _, c := range codes {
			if appErr.Code == c {
				return true
			}
		}
	}
	return false
}

// IsRetryable checks if error is retryable
func IsRetryable(err error) bool {
	retryableCodes := []string{
		"LLM_PROVIDER_UNAVAILABLE",
		"LLM_RATE_LIMITED",
		"LLM_TIMEOUT",
		"LLM_ALL_PROVIDERS_FAILED",
		"MT5_NOT_CONNECTED",
		"MT5_ORDER_FAILED",
		"DB_CONNECTION_FAILED",
		"DB_QUERY_FAILED",
		"SERVICE_UNAVAILABLE",
		"GATEWAY_TIMEOUT",
		"CIRCUIT_OPEN",
		"REQUEST_TIMEOUT",
	}
	return IsAnyCode(err, retryableCodes...)
}

// IsClientError checks if error is a client error (4xx)
func IsClientError(err error) bool {
	clientCodes := []string{
		"BAD_REQUEST",
		"UNAUTHORIZED",
		"FORBIDDEN",
		"NOT_FOUND",
		"VALIDATION_FAILED",
		"INVALID_INPUT",
		"MISSING_FIELD",
		"INVALID_SYMBOL",
		"INVALID_ORDER",
		"INVALID_ORDER_TYPE",
		"INVALID_ORDER_STATUS",
		"TOOL_INVALID_ARGS",
		"INVALID_ARGUMENT",
	}
	return IsAnyCode(err, clientCodes...)
}

// IsServerError checks if error is a server error (5xx)
func IsServerError(err error) bool {
	serverCodes := []string{
		"INTERNAL_SERVER_ERROR",
		"SERVICE_UNAVAILABLE",
		"GATEWAY_TIMEOUT",
		"DB_CONNECTION_FAILED",
		"DB_QUERY_FAILED",
		"LLM_PROVIDER_UNAVAILABLE",
		"MT5_NOT_CONNECTED",
		"MT5_ORDER_FAILED",
		"REQUEST_TIMEOUT",
	}
	return IsAnyCode(err, serverCodes...)
}

// NewValidationError creates a validation error with details
func NewValidationError(field, message string) *AppError {
	return New("VALIDATION_FAILED", "validation failed").WithMeta(field, message)
}

// NewNotFoundError creates a not found error
func NewNotFoundError(resource string) *AppError {
	return New("NOT_FOUND", resource+" not found")
}

// NewInternalError creates an internal server error
func NewInternalError(err error) *AppError {
	return New("INTERNAL_SERVER_ERROR", "internal server error").WithError(err)
}

// NewTimeoutError creates a timeout error
func NewTimeoutError(operation string) *AppError {
	return New("REQUEST_TIMEOUT", operation+" timed out")
}

// ErrorCode returns the error code for standard errors
func ErrorCode(err error) string {
	if appErr, ok := AsAppError(err); ok {
		return appErr.Code
	}
	return "UNKNOWN_ERROR"
}