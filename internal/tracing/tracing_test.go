package tracing

import (
	"context"
	"os"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func TestGetVersion(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected string
	}{
		{
			name:     "with SERVICE_VERSION set",
			envValue: "v1.2.3",
			expected: "v1.2.3",
		},
		{
			name:     "with empty SERVICE_VERSION",
			envValue: "",
			expected: "dev",
		},
		{
			name:     "with SERVICE_VERSION not set",
			envValue: "",
			expected: "dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv("SERVICE_VERSION", tt.envValue)
				defer os.Unsetenv("SERVICE_VERSION")
			} else {
				os.Unsetenv("SERVICE_VERSION")
			}

			result := getVersion()
			if result != tt.expected {
				t.Errorf("getVersion() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGetInstanceID(t *testing.T) {
	tests := []struct {
		name         string
		hostnameEnv  string
		podNameEnv   string
		expected     string
	}{
		{
			name:        "with HOSTNAME set",
			hostnameEnv: "web-server-01",
			podNameEnv:  "",
			expected:    "web-server-01",
		},
		{
			name:        "with POD_NAME set (no HOSTNAME)",
			hostnameEnv: "",
			podNameEnv:  "harborhook-worker-abc123",
			expected:    "harborhook-worker-abc123",
		},
		{
			name:        "with both HOSTNAME and POD_NAME set (HOSTNAME takes precedence)",
			hostnameEnv: "web-server-01",
			podNameEnv:  "harborhook-worker-abc123",
			expected:    "web-server-01",
		},
		{
			name:        "with neither set",
			hostnameEnv: "",
			podNameEnv:  "",
			expected:    "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up environment
			os.Unsetenv("HOSTNAME")
			os.Unsetenv("POD_NAME")

			// Set test environment
			if tt.hostnameEnv != "" {
				os.Setenv("HOSTNAME", tt.hostnameEnv)
				defer os.Unsetenv("HOSTNAME")
			}
			if tt.podNameEnv != "" {
				os.Setenv("POD_NAME", tt.podNameEnv)
				defer os.Unsetenv("POD_NAME")
			}

			result := getInstanceID()
			if result != tt.expected {
				t.Errorf("getInstanceID() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGetOTLPEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected string
	}{
		{
			name:     "with http:// prefix",
			envValue: "http://tempo:4318",
			expected: "tempo:4318",
		},
		{
			name:     "with https:// prefix",
			envValue: "https://tempo:4318",
			expected: "tempo:4318",
		},
		{
			name:     "without protocol prefix",
			envValue: "tempo:4318",
			expected: "tempo:4318",
		},
		{
			name:     "with custom endpoint",
			envValue: "otel-collector.monitoring.svc.cluster.local:4318",
			expected: "otel-collector.monitoring.svc.cluster.local:4318",
		},
		{
			name:     "empty environment variable",
			envValue: "",
			expected: "tempo:4318",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", tt.envValue)
				defer os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")
			} else {
				os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")
			}

			result := getOTLPEndpoint()
			if result != tt.expected {
				t.Errorf("getOTLPEndpoint() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGetTracer(t *testing.T) {
	tracer := GetTracer()
	if tracer == nil {
		t.Error("GetTracer() returned nil")
	}

	// Test that tracer has the expected instrumentation name
	// We can't directly access the name, but we can start a span and verify it works
	ctx := context.Background()
	_, span := tracer.Start(ctx, "test-span")
	if span == nil {
		t.Error("GetTracer().Start() returned nil span")
	}
	span.End()
}

func TestStartSpan(t *testing.T) {
	tests := []struct {
		name     string
		spanName string
		attrs    []attribute.KeyValue
	}{
		{
			name:     "simple span without attributes",
			spanName: "test-operation",
			attrs:    nil,
		},
		{
			name:     "span with single attribute",
			spanName: "database-query",
			attrs:    []attribute.KeyValue{attribute.String("db.table", "users")},
		},
		{
			name:     "span with multiple attributes",
			spanName: "http-request",
			attrs: []attribute.KeyValue{
				attribute.String("http.method", "POST"),
				attribute.String("http.url", "/api/webhooks"),
				attribute.Int("http.status_code", 200),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			newCtx, span := StartSpan(ctx, tt.spanName, tt.attrs...)

			if newCtx == nil {
				t.Error("StartSpan() returned nil context")
			}
			if span == nil {
				t.Error("StartSpan() returned nil span")
			}

			// Verify span is in context
			spanFromCtx := oteltrace.SpanFromContext(newCtx)
			if spanFromCtx == nil {
				t.Error("StartSpan() span not found in returned context")
			}

			span.End()
		})
	}
}

func TestAddSpanEvent(t *testing.T) {
	// Set up a test tracer to capture spans
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)

	tests := []struct {
		name      string
		eventName string
		attrs     []attribute.KeyValue
		hasSpan   bool
	}{
		{
			name:      "event with span in context",
			eventName: "processing-started",
			attrs:     []attribute.KeyValue{attribute.String("task.id", "task-123")},
			hasSpan:   true,
		},
		{
			name:      "event without span in context",
			eventName: "processing-started",
			attrs:     []attribute.KeyValue{attribute.String("task.id", "task-456")},
			hasSpan:   false,
		},
		{
			name:      "event with multiple attributes",
			eventName: "retry-attempt",
			attrs: []attribute.KeyValue{
				attribute.Int("attempt.number", 3),
				attribute.String("error.reason", "timeout"),
			},
			hasSpan: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			if tt.hasSpan {
				var span oteltrace.Span
				ctx, span = StartSpan(ctx, "test-span")
				defer span.End()
			}

			// This should not panic regardless of whether span exists
			AddSpanEvent(ctx, tt.eventName, tt.attrs...)
		})
	}
}

func TestSetSpanError(t *testing.T) {
	// Set up a test tracer to capture spans
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)

	tests := []struct {
		name    string
		err     error
		hasSpan bool
	}{
		{
			name:    "error with span in context",
			err:     context.DeadlineExceeded,
			hasSpan: true,
		},
		{
			name:    "error without span in context",
			err:     context.Canceled,
			hasSpan: false,
		},
		{
			name:    "nil error with span",
			err:     nil,
			hasSpan: true,
		},
		{
			name:    "nil error without span",
			err:     nil,
			hasSpan: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			if tt.hasSpan {
				var span oteltrace.Span
				ctx, span = StartSpan(ctx, "test-span")
				defer span.End()
			}

			// This should not panic regardless of whether span exists or error is nil
			SetSpanError(ctx, tt.err)
		})
	}
}

func TestGetTraceID(t *testing.T) {
	// Set up a test tracer
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)

	tests := []struct {
		name     string
		hasSpan  bool
		expected string
	}{
		{
			name:     "context with valid span",
			hasSpan:  true,
			expected: "", // We'll check it's not empty
		},
		{
			name:     "context without span",
			hasSpan:  false,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			if tt.hasSpan {
				var span oteltrace.Span
				ctx, span = StartSpan(ctx, "test-span")
				defer span.End()
			}

			traceID := GetTraceID(ctx)

			if tt.hasSpan {
				if traceID == "" {
					t.Error("GetTraceID() returned empty string for context with span")
				}
				if len(traceID) != 32 { // Trace ID should be 32 hex characters
					t.Errorf("GetTraceID() returned trace ID with unexpected length: got %d, want 32", len(traceID))
				}
			} else {
				if traceID != "" {
					t.Errorf("GetTraceID() returned %q for context without span, want empty string", traceID)
				}
			}
		})
	}
}

func TestPropagateTraceToNSQ(t *testing.T) {
	// Set up a test tracer
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	tests := []struct {
		name    string
		hasSpan bool
	}{
		{
			name:    "context with active span",
			hasSpan: true,
		},
		{
			name:    "context without span",
			hasSpan: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			if tt.hasSpan {
				var span oteltrace.Span
				ctx, span = StartSpan(ctx, "test-span")
				defer span.End()
			}

			headers := PropagateTraceToNSQ(ctx)

			if headers == nil {
				t.Error("PropagateTraceToNSQ() returned nil headers")
			}

			if tt.hasSpan {
				// Should have trace context headers when span is present
				if len(headers) == 0 {
					t.Error("PropagateTraceToNSQ() returned empty headers for context with span")
				}

				// Check for expected trace context header
				found := false
				for key := range headers {
					if strings.Contains(strings.ToLower(key), "trace") {
						found = true
						break
					}
				}
				if !found {
					t.Error("PropagateTraceToNSQ() did not include trace context headers")
				}
			} else {
				// May or may not have headers when no span is present - this is acceptable
				// The important thing is it doesn't crash
			}
		})
	}
}

func TestExtractTraceFromNSQ(t *testing.T) {
	// Set up a test tracer
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	tests := []struct {
		name    string
		headers map[string]string
	}{
		{
			name:    "empty headers",
			headers: map[string]string{},
		},
		{
			name: "headers with trace context",
			headers: map[string]string{
				"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			},
		},
		{
			name: "headers with trace context and baggage",
			headers: map[string]string{
				"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
				"baggage":     "key1=value1,key2=value2",
			},
		},
		{
			name: "headers with invalid trace context",
			headers: map[string]string{
				"traceparent": "invalid-trace-context",
			},
		},
		{
			name:    "nil headers",
			headers: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// This should not panic regardless of header content
			newCtx := ExtractTraceFromNSQ(ctx, tt.headers)

			if newCtx == nil {
				t.Error("ExtractTraceFromNSQ() returned nil context")
			}

			// Verify we got a context back (even if trace extraction failed)
			if newCtx == ctx && len(tt.headers) > 0 {
				// If headers were provided, we should get a new context
				// (though this isn't strictly required by the implementation)
			}
		})
	}
}

func TestTraceRoundTrip(t *testing.T) {
	// Set up a test tracer
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Create a span and propagate it
	ctx := context.Background()
	ctx, span := StartSpan(ctx, "test-operation")
	defer span.End()

	originalTraceID := GetTraceID(ctx)
	if originalTraceID == "" {
		t.Fatal("Failed to get trace ID from original context")
	}

	// Propagate to NSQ headers
	headers := PropagateTraceToNSQ(ctx)
	if len(headers) == 0 {
		t.Fatal("PropagateTraceToNSQ() returned empty headers")
	}

	// Extract from NSQ headers
	newCtx := ExtractTraceFromNSQ(context.Background(), headers)

	// Start a child span to activate the trace context
	newCtx, childSpan := StartSpan(newCtx, "child-operation")
	defer childSpan.End()

	extractedTraceID := GetTraceID(newCtx)

	// The trace ID should be the same after round-trip
	if extractedTraceID != originalTraceID {
		t.Errorf("Trace ID changed during round-trip: original=%s, extracted=%s", originalTraceID, extractedTraceID)
	}
}

func TestTracerNameConstant(t *testing.T) {
	expected := "github.com/austindbirch/harbor_hook"
	if TracerName != expected {
		t.Errorf("TracerName constant = %q, want %q", TracerName, expected)
	}
}