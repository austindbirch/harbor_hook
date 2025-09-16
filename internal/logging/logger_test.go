package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name        string
		serviceName string
	}{
		{
			name:        "create logger with service name",
			serviceName: "test-service",
		},
		{
			name:        "create logger with empty service name",
			serviceName: "",
		},
		{
			name:        "create logger with complex service name",
			serviceName: "harbor-hook-worker-v2.1.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := New(tt.serviceName)

			if logger == nil {
				t.Error("New() returned nil logger")
			}
			if logger.service != tt.serviceName {
				t.Errorf("New() service = %q, want %q", logger.service, tt.serviceName)
			}
		})
	}
}

func TestLogger_WithContext(t *testing.T) {
	// Set up test tracer for trace ID extraction
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)

	tests := []struct {
		name        string
		serviceName string
		hasTrace    bool
	}{
		{
			name:        "with trace context",
			serviceName: "test-service",
			hasTrace:    true,
		},
		{
			name:        "without trace context",
			serviceName: "test-service",
			hasTrace:    false,
		},
		{
			name:        "empty service name with trace",
			serviceName: "",
			hasTrace:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := New(tt.serviceName)
			ctx := context.Background()

			if tt.hasTrace {
				tracer := otel.Tracer("test-tracer")
				newCtx, span := tracer.Start(ctx, "test-span")
				ctx = newCtx
				defer span.End()
			}

			before := time.Now().UTC()
			entry := logger.WithContext(ctx)
			after := time.Now().UTC()

			if entry == nil {
				t.Error("WithContext() returned nil entry")
			}
			if entry.Service != tt.serviceName {
				t.Errorf("WithContext() Service = %q, want %q", entry.Service, tt.serviceName)
			}
			if entry.Time.Before(before) || entry.Time.After(after) {
				t.Errorf("WithContext() Time %v not between %v and %v", entry.Time, before, after)
			}
			if entry.Fields == nil {
				t.Error("WithContext() Fields should not be nil")
			}

			if tt.hasTrace {
				if entry.TraceID == "" {
					t.Error("WithContext() TraceID should not be empty with trace context")
				}
			} else {
				if entry.TraceID != "" {
					t.Errorf("WithContext() TraceID = %q, want empty string without trace", entry.TraceID)
				}
			}
		})
	}
}

func TestLogger_WithFields(t *testing.T) {
	tests := []struct {
		name        string
		serviceName string
		fields      map[string]any
	}{
		{
			name:        "with string fields",
			serviceName: "test-service",
			fields:      map[string]any{"key1": "value1", "key2": "value2"},
		},
		{
			name:        "with mixed type fields",
			serviceName: "test-service",
			fields:      map[string]any{"count": 42, "active": true, "name": "test"},
		},
		{
			name:        "with empty fields",
			serviceName: "test-service",
			fields:      map[string]any{},
		},
		{
			name:        "with nil fields",
			serviceName: "test-service",
			fields:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := New(tt.serviceName)

			before := time.Now().UTC()
			entry := logger.WithFields(tt.fields)
			after := time.Now().UTC()

			if entry == nil {
				t.Error("WithFields() returned nil entry")
			}
			if entry.Service != tt.serviceName {
				t.Errorf("WithFields() Service = %q, want %q", entry.Service, tt.serviceName)
			}
			if entry.Time.Before(before) || entry.Time.After(after) {
				t.Errorf("WithFields() Time %v not between %v and %v", entry.Time, before, after)
			}

			if tt.fields == nil {
				if entry.Fields != nil {
					t.Error("WithFields() Fields should be nil when input is nil")
				}
			} else {
				if len(entry.Fields) != len(tt.fields) {
					t.Errorf("WithFields() Fields length = %d, want %d", len(entry.Fields), len(tt.fields))
				}
				for k, v := range tt.fields {
					if entry.Fields[k] != v {
						t.Errorf("WithFields() Fields[%q] = %v, want %v", k, entry.Fields[k], v)
					}
				}
			}
		})
	}
}

func TestLogger_Plain(t *testing.T) {
	tests := []struct {
		name        string
		serviceName string
	}{
		{
			name:        "plain entry with service",
			serviceName: "test-service",
		},
		{
			name:        "plain entry with empty service",
			serviceName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := New(tt.serviceName)

			before := time.Now().UTC()
			entry := logger.Plain()
			after := time.Now().UTC()

			if entry == nil {
				t.Error("Plain() returned nil entry")
			}
			if entry.Service != tt.serviceName {
				t.Errorf("Plain() Service = %q, want %q", entry.Service, tt.serviceName)
			}
			if entry.Time.Before(before) || entry.Time.After(after) {
				t.Errorf("Plain() Time %v not between %v and %v", entry.Time, before, after)
			}
			if entry.Fields == nil {
				t.Error("Plain() Fields should not be nil")
			}
			if len(entry.Fields) != 0 {
				t.Errorf("Plain() Fields should be empty, got %v", entry.Fields)
			}
		})
	}
}

func TestLogEntry_FluentMethods(t *testing.T) {
	tests := []struct {
		name     string
		setupFn  func(*LogEntry) *LogEntry
		checkFn  func(*testing.T, *LogEntry)
	}{
		{
			name: "WithTraceID",
			setupFn: func(e *LogEntry) *LogEntry {
				return e.WithTraceID("trace-123")
			},
			checkFn: func(t *testing.T, e *LogEntry) {
				if e.TraceID != "trace-123" {
					t.Errorf("WithTraceID() TraceID = %q, want %q", e.TraceID, "trace-123")
				}
			},
		},
		{
			name: "WithTenant",
			setupFn: func(e *LogEntry) *LogEntry {
				return e.WithTenant("tenant-456")
			},
			checkFn: func(t *testing.T, e *LogEntry) {
				if e.TenantID != "tenant-456" {
					t.Errorf("WithTenant() TenantID = %q, want %q", e.TenantID, "tenant-456")
				}
			},
		},
		{
			name: "WithEvent",
			setupFn: func(e *LogEntry) *LogEntry {
				return e.WithEvent("event-789")
			},
			checkFn: func(t *testing.T, e *LogEntry) {
				if e.EventID != "event-789" {
					t.Errorf("WithEvent() EventID = %q, want %q", e.EventID, "event-789")
				}
			},
		},
		{
			name: "WithDelivery",
			setupFn: func(e *LogEntry) *LogEntry {
				return e.WithDelivery("delivery-abc")
			},
			checkFn: func(t *testing.T, e *LogEntry) {
				if e.DeliveryID != "delivery-abc" {
					t.Errorf("WithDelivery() DeliveryID = %q, want %q", e.DeliveryID, "delivery-abc")
				}
			},
		},
		{
			name: "WithEndpoint",
			setupFn: func(e *LogEntry) *LogEntry {
				return e.WithEndpoint("endpoint-def")
			},
			checkFn: func(t *testing.T, e *LogEntry) {
				if e.EndpointID != "endpoint-def" {
					t.Errorf("WithEndpoint() EndpointID = %q, want %q", e.EndpointID, "endpoint-def")
				}
			},
		},
		{
			name: "chained methods",
			setupFn: func(e *LogEntry) *LogEntry {
				return e.WithTraceID("trace-123").WithTenant("tenant-456").WithEvent("event-789")
			},
			checkFn: func(t *testing.T, e *LogEntry) {
				if e.TraceID != "trace-123" {
					t.Errorf("Chained TraceID = %q, want %q", e.TraceID, "trace-123")
				}
				if e.TenantID != "tenant-456" {
					t.Errorf("Chained TenantID = %q, want %q", e.TenantID, "tenant-456")
				}
				if e.EventID != "event-789" {
					t.Errorf("Chained EventID = %q, want %q", e.EventID, "event-789")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := New("test-service")
			entry := logger.Plain()

			result := tt.setupFn(entry)

			// Verify fluent interface returns same entry
			if result != entry {
				t.Error("Fluent method should return same LogEntry instance")
			}

			tt.checkFn(t, entry)
		})
	}
}

func TestLogEntry_WithField(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value any
	}{
		{
			name:  "string value",
			key:   "operation",
			value: "webhook-delivery",
		},
		{
			name:  "integer value",
			key:   "attempt",
			value: 3,
		},
		{
			name:  "boolean value",
			key:   "success",
			value: true,
		},
		{
			name:  "nil value",
			key:   "nullable",
			value: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := New("test-service")
			entry := logger.Plain()

			result := entry.WithField(tt.key, tt.value)

			if result != entry {
				t.Error("WithField() should return same LogEntry instance")
			}
			if entry.Fields == nil {
				t.Error("WithField() Fields should not be nil after adding field")
			}
			if entry.Fields[tt.key] != tt.value {
				t.Errorf("WithField() Fields[%q] = %v, want %v", tt.key, entry.Fields[tt.key], tt.value)
			}
		})
	}
}

func TestLogEntry_WithFields(t *testing.T) {
	tests := []struct {
		name         string
		initialFields map[string]any
		newFields     map[string]any
		expectedLen   int
	}{
		{
			name:         "add fields to empty entry",
			initialFields: nil,
			newFields:     map[string]any{"key1": "value1", "key2": 42},
			expectedLen:   2,
		},
		{
			name:         "add fields to existing fields",
			initialFields: map[string]any{"existing": "value"},
			newFields:     map[string]any{"key1": "value1", "key2": 42},
			expectedLen:   3,
		},
		{
			name:         "overwrite existing fields",
			initialFields: map[string]any{"key1": "old"},
			newFields:     map[string]any{"key1": "new", "key2": 42},
			expectedLen:   2,
		},
		{
			name:         "add empty fields map",
			initialFields: map[string]any{"existing": "value"},
			newFields:     map[string]any{},
			expectedLen:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := New("test-service")
			entry := logger.WithFields(tt.initialFields)

			result := entry.WithFields(tt.newFields)

			if result != entry {
				t.Error("WithFields() should return same LogEntry instance")
			}
			if len(entry.Fields) != tt.expectedLen {
				t.Errorf("WithFields() Fields length = %d, want %d", len(entry.Fields), tt.expectedLen)
			}

			// Verify new fields are present
			for k, v := range tt.newFields {
				if entry.Fields[k] != v {
					t.Errorf("WithFields() Fields[%q] = %v, want %v", k, entry.Fields[k], v)
				}
			}
		})
	}
}

func TestLogEntry_WithError(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "with error",
			err:  fmt.Errorf("test error message"),
		},
		{
			name: "with nil error",
			err:  nil,
		},
		{
			name: "with wrapped error",
			err:  fmt.Errorf("wrapped: %w", fmt.Errorf("original error")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := New("test-service")
			entry := logger.Plain()

			result := entry.WithError(tt.err)

			if result != entry {
				t.Error("WithError() should return same LogEntry instance")
			}

			if tt.err != nil {
				if entry.Fields == nil {
					t.Error("WithError() Fields should not be nil after adding error")
				}
				if entry.Fields["error"] != tt.err.Error() {
					t.Errorf("WithError() Fields[\"error\"] = %v, want %v", entry.Fields["error"], tt.err.Error())
				}
			} else {
				// With nil error, the error field should not be added
				if entry.Fields != nil && entry.Fields["error"] != nil {
					t.Error("WithError() should not add error field for nil error")
				}
			}
		})
	}
}

func TestLogEntry_LoggingMethods(t *testing.T) {
	// Capture stdout for testing
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	defer func() {
		os.Stdout = oldStdout
	}()

	tests := []struct {
		name          string
		setupFn       func(*LogEntry)
		expectedLevel LogLevel
		expectedMsg   string
	}{
		{
			name:          "Debug",
			setupFn:       func(e *LogEntry) { e.Debug("debug message") },
			expectedLevel: LevelDebug,
			expectedMsg:   "debug message",
		},
		{
			name:          "Debugf",
			setupFn:       func(e *LogEntry) { e.Debugf("debug %s %d", "formatted", 123) },
			expectedLevel: LevelDebug,
			expectedMsg:   "debug formatted 123",
		},
		{
			name:          "Info",
			setupFn:       func(e *LogEntry) { e.Info("info message") },
			expectedLevel: LevelInfo,
			expectedMsg:   "info message",
		},
		{
			name:          "Infof",
			setupFn:       func(e *LogEntry) { e.Infof("info %s", "formatted") },
			expectedLevel: LevelInfo,
			expectedMsg:   "info formatted",
		},
		{
			name:          "Warn",
			setupFn:       func(e *LogEntry) { e.Warn("warn message") },
			expectedLevel: LevelWarn,
			expectedMsg:   "warn message",
		},
		{
			name:          "Warnf",
			setupFn:       func(e *LogEntry) { e.Warnf("warn %d", 456) },
			expectedLevel: LevelWarn,
			expectedMsg:   "warn 456",
		},
		{
			name:          "Error",
			setupFn:       func(e *LogEntry) { e.Error("error message") },
			expectedLevel: LevelError,
			expectedMsg:   "error message",
		},
		{
			name:          "Errorf",
			setupFn:       func(e *LogEntry) { e.Errorf("error %v", true) },
			expectedLevel: LevelError,
			expectedMsg:   "error true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := New("test-service")
			entry := logger.Plain().WithField("test", "value")

			// Create a goroutine to capture output
			outputChan := make(chan string, 1)
			go func() {
				var buf bytes.Buffer
				io.Copy(&buf, r)
				outputChan <- buf.String()
			}()

			tt.setupFn(entry)

			// Close writer and read output
			w.Close()
			output := <-outputChan

			// Parse the JSON output
			var loggedEntry LogEntry
			err := json.Unmarshal([]byte(strings.TrimSpace(output)), &loggedEntry)
			if err != nil {
				t.Errorf("Failed to parse JSON output: %v", err)
			}

			if loggedEntry.Level != tt.expectedLevel {
				t.Errorf("Log Level = %q, want %q", loggedEntry.Level, tt.expectedLevel)
			}
			if loggedEntry.Message != tt.expectedMsg {
				t.Errorf("Log Message = %q, want %q", loggedEntry.Message, tt.expectedMsg)
			}
			if loggedEntry.Service != "test-service" {
				t.Errorf("Log Service = %q, want %q", loggedEntry.Service, "test-service")
			}

			// Restore stdout for next test
			r, w, _ = os.Pipe()
			os.Stdout = w
		})
	}
}

func TestGlobalFunctions(t *testing.T) {
	tests := []struct {
		name    string
		testFn  func() *LogEntry
		checkFn func(*testing.T, *LogEntry)
	}{
		{
			name:   "WithContext global function",
			testFn: func() *LogEntry { return WithContext(context.Background()) },
			checkFn: func(t *testing.T, e *LogEntry) {
				if e.Service != defaultLogger.service {
					t.Errorf("Global WithContext() Service = %q, want %q", e.Service, defaultLogger.service)
				}
			},
		},
		{
			name:   "WithFields global function",
			testFn: func() *LogEntry { return WithFields(map[string]any{"key": "value"}) },
			checkFn: func(t *testing.T, e *LogEntry) {
				if e.Service != defaultLogger.service {
					t.Errorf("Global WithFields() Service = %q, want %q", e.Service, defaultLogger.service)
				}
				if e.Fields["key"] != "value" {
					t.Errorf("Global WithFields() Fields[\"key\"] = %v, want %v", e.Fields["key"], "value")
				}
			},
		},
		{
			name:   "Plain global function",
			testFn: func() *LogEntry { return Plain() },
			checkFn: func(t *testing.T, e *LogEntry) {
				if e.Service != defaultLogger.service {
					t.Errorf("Global Plain() Service = %q, want %q", e.Service, defaultLogger.service)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := tt.testFn()

			if entry == nil {
				t.Error("Global function returned nil entry")
			}

			tt.checkFn(t, entry)
		})
	}
}

func TestSetDefaultService(t *testing.T) {
	originalService := defaultLogger.service
	defer func() {
		defaultLogger.service = originalService
	}()

	tests := []struct {
		name        string
		serviceName string
	}{
		{
			name:        "set custom service name",
			serviceName: "custom-service",
		},
		{
			name:        "set empty service name",
			serviceName: "",
		},
		{
			name:        "set complex service name",
			serviceName: "harbor-hook-ingest-v1.2.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetDefaultService(tt.serviceName)

			if defaultLogger.service != tt.serviceName {
				t.Errorf("SetDefaultService() service = %q, want %q", defaultLogger.service, tt.serviceName)
			}

			// Test that global functions use the new service name
			entry := Plain()
			if entry.Service != tt.serviceName {
				t.Errorf("Plain() after SetDefaultService() Service = %q, want %q", entry.Service, tt.serviceName)
			}
		})
	}
}

func TestLogLevelConstants(t *testing.T) {
	tests := []struct {
		name     string
		level    LogLevel
		expected string
	}{
		{"LevelDebug", LevelDebug, "debug"},
		{"LevelInfo", LevelInfo, "info"},
		{"LevelWarn", LevelWarn, "warn"},
		{"LevelError", LevelError, "error"},
		{"LevelFatal", LevelFatal, "fatal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.level) != tt.expected {
				t.Errorf("LogLevel %s = %q, want %q", tt.name, string(tt.level), tt.expected)
			}
		})
	}
}

func TestLogEntryJSONSerialization(t *testing.T) {
	tests := []struct {
		name  string
		entry LogEntry
	}{
		{
			name: "complete log entry",
			entry: LogEntry{
				Time:       time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Level:      LevelInfo,
				Message:    "test message",
				Service:    "test-service",
				TraceID:    "trace-123",
				SpanID:     "span-456",
				TenantID:   "tenant-789",
				EventID:    "event-abc",
				DeliveryID: "delivery-def",
				EndpointID: "endpoint-ghi",
				Fields:     map[string]any{"key": "value", "count": 42},
			},
		},
		{
			name: "minimal log entry",
			entry: LogEntry{
				Time:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Level:   LevelError,
				Message: "error occurred",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test marshaling
			jsonData, err := json.Marshal(tt.entry)
			if err != nil {
				t.Errorf("LogEntry JSON marshal error: %v", err)
			}

			// Test unmarshaling
			var unmarshaled LogEntry
			err = json.Unmarshal(jsonData, &unmarshaled)
			if err != nil {
				t.Errorf("LogEntry JSON unmarshal error: %v", err)
			}

			// Verify round-trip integrity
			if unmarshaled.Level != tt.entry.Level {
				t.Errorf("JSON round-trip Level mismatch: got %q, want %q", unmarshaled.Level, tt.entry.Level)
			}
			if unmarshaled.Message != tt.entry.Message {
				t.Errorf("JSON round-trip Message mismatch: got %q, want %q", unmarshaled.Message, tt.entry.Message)
			}
			if unmarshaled.Service != tt.entry.Service {
				t.Errorf("JSON round-trip Service mismatch: got %q, want %q", unmarshaled.Service, tt.entry.Service)
			}
		})
	}
}