package logging

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

type ctxKey string

const (
	requestIDKey     ctxKey = "request_id"
	correlationIDKey ctxKey = "correlation_id"
	loggerKey        ctxKey = "logger"
)

var (
	defaultLogger *slog.Logger
	once          sync.Once
)

// Init initializes the global logger
func Init(level string, format string) {
	once.Do(func() {
		var lvl slog.Level
		switch strings.ToLower(level) {
		case "debug":
			lvl = slog.LevelDebug
		case "info":
			lvl = slog.LevelInfo
		case "warn", "warning":
			lvl = slog.LevelWarn
		case "error":
			lvl = slog.LevelError
		default:
			lvl = slog.LevelInfo
		}

		opts := &slog.HandlerOptions{
			Level:     lvl,
			AddSource: true,
			ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				if a.Key == slog.SourceKey {
					if source, ok := a.Value.Any().(*slog.Source); ok {
						source.File = shortFile(source.File)
					}
				}
				return a
			},
		}

		var handler slog.Handler
		switch strings.ToLower(format) {
		case "json":
			handler = slog.NewJSONHandler(os.Stdout, opts)
		case "text":
			handler = slog.NewTextHandler(os.Stdout, opts)
		default:
			handler = slog.NewJSONHandler(os.Stdout, opts)
		}

		defaultLogger = slog.New(handler)
		slog.SetDefault(defaultLogger)
	})
}

// Default returns the default logger
func Default() *slog.Logger {
	if defaultLogger == nil {
		Init("info", "json")
	}
	return defaultLogger
}

// WithRequestID adds request ID to context
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// RequestID extracts request ID from context
func RequestID(ctx context.Context) string {
	if v := ctx.Value(requestIDKey); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// WithCorrelationID adds correlation ID to context
func WithCorrelationID(ctx context.Context, correlationID string) context.Context {
	return context.WithValue(ctx, correlationIDKey, correlationID)
}

// CorrelationID extracts correlation ID from context
func CorrelationID(ctx context.Context) string {
	if v := ctx.Value(correlationIDKey); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// WithLogger adds logger to context
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// FromContext extracts logger from context, falls back to default
func FromContext(ctx context.Context) *slog.Logger {
	if v := ctx.Value(loggerKey); v != nil {
		if l, ok := v.(*slog.Logger); ok {
			return l
		}
	}
	return Default()
}

// Logger returns a logger with request/correlation IDs if present
func Logger(ctx context.Context) *slog.Logger {
	logger := FromContext(ctx)
	var args []any
	if rid := RequestID(ctx); rid != "" {
		args = append(args, "request_id", rid)
	}
	if cid := CorrelationID(ctx); cid != "" {
		args = append(args, "correlation_id", cid)
	}
	if len(args) > 0 {
		return logger.With(args...)
	}
	return logger
}

// WithFields adds structured fields to logger
func WithFields(ctx context.Context, fields map[string]any) *slog.Logger {
	logger := Logger(ctx)
	if len(fields) == 0 {
		return logger
	}
	args := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		args = append(args, k, v)
	}
	return logger.With(args...)
}

// shortFile returns the last 2 path components of a file path
func shortFile(file string) string {
	for i := len(file) - 1; i >= 0; i-- {
		if file[i] == '/' || file[i] == '\\' {
			for j := i - 1; j >= 0; j-- {
				if file[j] == '/' || file[j] == '\\' {
					return file[j+1:]
				}
			}
			return file[i+1:]
		}
	}
	return file
}

// Debug logs at debug level
func Debug(ctx context.Context, msg string, args ...any) {
	Logger(ctx).Debug(msg, args...)
}

// Info logs at info level
func Info(ctx context.Context, msg string, args ...any) {
	Logger(ctx).Info(msg, args...)
}

// Warn logs at warn level
func Warn(ctx context.Context, msg string, args ...any) {
	Logger(ctx).Warn(msg, args...)
}

// Error logs at error level
func Error(ctx context.Context, msg string, args ...any) {
	Logger(ctx).Error(msg, args...)
}

// ErrorWithStack logs error with stack trace
func ErrorWithStack(ctx context.Context, msg string, err error, args ...any) {
	allArgs := append([]any{"error", err.Error()}, args...)
	Logger(ctx).Error(msg, allArgs...)
}

// Panic logs at panic level then panics
func Panic(ctx context.Context, msg string, args ...any) {
	Logger(ctx).With("stack", stackTrace()).Error(msg, args...)
	os.Exit(1)
}

// Fatal logs at error level then exits (use sparingly, prefer returning errors)
func Fatal(ctx context.Context, msg string, args ...any) {
	Logger(ctx).Error(msg, args...)
	os.Exit(1)
}

// stackTrace returns a string of the current stack trace
func stackTrace() string {
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	return string(buf[:n])
}

// LogAttr creates a slog attribute
func LogAttr(key string, value any) slog.Attr {
	return slog.Any(key, value)
}

// LogAttrs creates multiple slog attributes
func LogAttrs(attrs ...slog.Attr) []any {
	args := make([]any, 0, len(attrs)*2)
	for _, a := range attrs {
		args = append(args, a.Key, a.Value.Any())
	}
	return args
}

// LogLevel represents log levels
type LogLevel string

const (
	LevelDebug LogLevel = "debug"
	LevelInfo  LogLevel = "info"
	LevelWarn  LogLevel = "warn"
	LevelError LogLevel = "error"
)

// WithDuration adds duration field to logger
func WithDuration(ctx context.Context, start time.Time) *slog.Logger {
	return Logger(ctx).With("duration_ms", time.Since(start).Milliseconds())
}

// WithError adds error field to logger
func WithError(ctx context.Context, err error) *slog.Logger {
	if err == nil {
		return Logger(ctx)
	}
	return Logger(ctx).With("error", err.Error())
}

// WithComponent adds component field to logger
func WithComponent(ctx context.Context, component string) *slog.Logger {
	return Logger(ctx).With("component", component)
}