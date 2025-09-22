package tracing

import (
	"context"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.20.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// TracerName is the instrumentation name for this application
const TracerName = "github.com/austindbirch/harbor_hook"

// InitTracing initializes OpenTelemetry tracing for the service
func InitTracing(ctx context.Context, serviceName string) (func(), error) {
	// Create resource with service information
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String(getVersion()),
			attribute.String("service.instance.id", getInstanceID()),
		),
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithOS(),
		resource.WithHost(),
	)
	if err != nil {
		return nil, err
	}

	// Create OTLP HTTP exporter
	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(getOTLPEndpoint()),
		otlptracehttp.WithInsecure(), // For development
	)
	if err != nil {
		return nil, err
	}

	// Create trace provider
	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(res),
		trace.WithSampler(trace.AlwaysSample()), // Sample all traces for development
	)

	// Set global trace provider and propagator
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Return shutdown function
	return func() {
		_ = tp.Shutdown(ctx)
	}, nil
}

// GetTracer returns a tracer for the Harborhook application
func GetTracer() oteltrace.Tracer {
	return otel.Tracer(TracerName)
}

// StartSpan starts a new span with the given name and attributes
func StartSpan(ctx context.Context, spanName string, attrs ...attribute.KeyValue) (context.Context, oteltrace.Span) {
	tracer := GetTracer()
	ctx, span := tracer.Start(ctx, spanName)

	if len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}

	return ctx, span
}

// AddSpanEvent adds an event to the current span
func AddSpanEvent(ctx context.Context, name string, attrs ...attribute.KeyValue) {
	span := oteltrace.SpanFromContext(ctx)
	if span != nil {
		span.AddEvent(name, oteltrace.WithAttributes(attrs...))
	}
}

// SetSpanError records an error on the current span
func SetSpanError(ctx context.Context, err error) {
	span := oteltrace.SpanFromContext(ctx)
	if span != nil && err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}

// GetTraceID extracts the trace ID from the context
func GetTraceID(ctx context.Context) string {
	span := oteltrace.SpanFromContext(ctx)
	if span != nil && span.SpanContext().IsValid() {
		return span.SpanContext().TraceID().String()
	}
	return ""
}

// getVersion returns the service version from environment or default
func getVersion() string {
	if v := os.Getenv("SERVICE_VERSION"); v != "" {
		return v
	}
	return "dev"
}

// getInstanceID returns a unique instance identifier
func getInstanceID() string {
	if id := os.Getenv("HOSTNAME"); id != "" {
		return id
	}
	if id := os.Getenv("POD_NAME"); id != "" {
		return id
	}
	return "unknown"
}

// getOTLPEndpoint returns the OTLP endpoint URL
func getOTLPEndpoint() string {
	if endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); endpoint != "" {
		// Remove http:// or https:// prefix if present since otlptracehttp.WithEndpoint expects just host:port
		if strings.HasPrefix(endpoint, "http://") {
			return strings.TrimPrefix(endpoint, "http://")
		}
		if strings.HasPrefix(endpoint, "https://") {
			return strings.TrimPrefix(endpoint, "https://")
		}
		return endpoint
	}
	// Default to Tempo in docker-compose (host:port only)
	return "tempo:4318"
}

// PropagateTraceToNSQ extracts trace context and returns it as a map for NSQ message headers
func PropagateTraceToNSQ(ctx context.Context) map[string]string {
	headers := make(map[string]string)
	propagator := otel.GetTextMapPropagator()
	propagator.Inject(ctx, propagation.MapCarrier(headers))
	return headers
}

// ExtractTraceFromNSQ extracts trace context from NSQ message headers
func ExtractTraceFromNSQ(ctx context.Context, headers map[string]string) context.Context {
	propagator := otel.GetTextMapPropagator()
	return propagator.Extract(ctx, propagation.MapCarrier(headers))
}
