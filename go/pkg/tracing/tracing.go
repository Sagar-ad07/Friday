package tracing

import (
	"context"
	"log"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Config holds tracing configuration
type Config struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	Endpoint       string
	Insecure       bool
	SampleRate     float64
	EnableStdout   bool
}

// DefaultConfig returns default tracing config
func DefaultConfig() *Config {
	return &Config{
		ServiceName:    "friday-go",
		ServiceVersion: "dev",
		Environment:    "development",
		Endpoint:       "",
		Insecure:       true,
		SampleRate:     1.0,
		EnableStdout:   false,
	}
}

// InitTracer initializes OpenTelemetry tracer provider
func InitTracer(ctx context.Context, cfg *Config) (func(context.Context) error, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	var exporters []sdktrace.SpanExporter

	// OTLP HTTP exporter
	if cfg.Endpoint != "" {
		exp, err := otlptracehttp.New(ctx,
			otlptracehttp.WithEndpoint(cfg.Endpoint),
			otlptracehttp.WithInsecure(),
		)
		if err != nil {
			return nil, err
		}
		exporters = append(exporters, exp)
		log.Printf("OTLP trace exporter configured: %s", cfg.Endpoint)
	}

	// Stdout exporter for development
	if cfg.EnableStdout {
		exp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, err
		}
		exporters = append(exporters, exp)
		log.Println("Stdout trace exporter enabled")
	}

	// If no exporters, use no-op
	if len(exporters) == 0 {
		log.Println("No trace exporters configured, using no-op tracer")
		tp := sdktrace.NewTracerProvider()
		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))
		return tp.Shutdown, nil
	}

	// Create resource
	res, err := resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", cfg.ServiceName),
			attribute.String("service.version", cfg.ServiceVersion),
			attribute.String("deployment.environment", cfg.Environment),
		),
	)
	if err != nil {
		return nil, err
	}

	// Configure sampler
	sampler := sdktrace.AlwaysSample()
	if cfg.SampleRate < 1.0 {
		sampler = sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRate))
	}

	// Build tracer provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporters[0]),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// Add additional exporters if any
	for i := 1; i < len(exporters); i++ {
		tp.RegisterSpanProcessor(sdktrace.NewBatchSpanProcessor(exporters[i]))
	}

	// Set global tracer provider
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	log.Printf("Tracer initialized: service=%s, version=%s, env=%s", cfg.ServiceName, cfg.ServiceVersion, cfg.Environment)
	return tp.Shutdown, nil
}

// Tracer returns a tracer for the given name
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

// StartSpan starts a new span
func StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return Tracer("").Start(ctx, name, opts...)
}

// AddSpanAttributes adds attributes to current span
func AddSpanAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		span.SetAttributes(attrs...)
	}
}

// RecordError records an error on the current span
func RecordError(ctx context.Context, err error, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() && err != nil {
		span.RecordError(err, trace.WithAttributes(attrs...))
	}
}

// SetSpanStatus sets the span status
func SetSpanStatus(ctx context.Context, code codes.Code, description string) {
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		span.SetStatus(code, description)
	}
}

// SetSpanOK marks span as successful
func SetSpanOK(ctx context.Context) {
	SetSpanStatus(ctx, codes.Ok, "")
}

// SetSpanError marks span as error with message
func SetSpanError(ctx context.Context, message string) {
	SetSpanStatus(ctx, codes.Error, message)
}

// IsRecording checks if the current span is recording
func IsRecording(ctx context.Context) bool {
	span := trace.SpanFromContext(ctx)
	return span.SpanContext().IsValid() && span.IsRecording()
}

func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}