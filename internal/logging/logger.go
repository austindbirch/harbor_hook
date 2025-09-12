package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/austindbirch/harbor_hook/internal/tracing"
)

// LogLevel represents the severity of the log entry
type LogLevel string

const (
	LevelDebug LogLevel = "debug"
	LevelInfo  LogLevel = "info"
	LevelWarn  LogLevel = "warn"
	LevelError LogLevel = "error"
	LevelFatal LogLevel = "fatal"
)

// LogEntry represents a structured log entry
type LogEntry struct {
	Time       time.Time         `json:"time"`
	Level      LogLevel          `json:"level"`
	Message    string            `json:"msg"`
	Service    string            `json:"service,omitempty"`
	TraceID    string            `json:"trace_id,omitempty"`
	SpanID     string            `json:"span_id,omitempty"`
	TenantID   string            `json:"tenant_id,omitempty"`
	EventID    string            `json:"event_id,omitempty"`
	DeliveryID string            `json:"delivery_id,omitempty"`
	EndpointID string            `json:"endpoint_id,omitempty"`
	Fields     map[string]any    `json:"fields,omitempty"`
}

// Logger provides structured logging with trace correlation
type Logger struct {
	service string
}

// New creates a new structured logger for the given service
func New(service string) *Logger {
	return &Logger{
		service: service,
	}
}

// WithContext creates a log entry with trace correlation from context
func (l *Logger) WithContext(ctx context.Context) *LogEntry {
	entry := &LogEntry{
		Time:    time.Now().UTC(),
		Service: l.service,
		Fields:  make(map[string]any),
	}
	
	// Extract trace information from context
	if traceID := tracing.GetTraceID(ctx); traceID != "" {
		entry.TraceID = traceID
	}
	
	return entry
}

// WithFields creates a log entry with arbitrary key-value pairs
func (l *Logger) WithFields(fields map[string]any) *LogEntry {
	entry := &LogEntry{
		Time:    time.Now().UTC(),
		Service: l.service,
		Fields:  fields,
	}
	return entry
}

// Plain creates a basic log entry without context
func (l *Logger) Plain() *LogEntry {
	return &LogEntry{
		Time:    time.Now().UTC(),
		Service: l.service,
		Fields:  make(map[string]any),
	}
}

// Fluent interface methods for LogEntry

// WithTraceID sets the trace ID for the log entry
func (e *LogEntry) WithTraceID(traceID string) *LogEntry {
	e.TraceID = traceID
	return e
}

// WithTenant sets the tenant ID for the log entry
func (e *LogEntry) WithTenant(tenantID string) *LogEntry {
	e.TenantID = tenantID
	return e
}

// WithEvent sets the event ID for the log entry
func (e *LogEntry) WithEvent(eventID string) *LogEntry {
	e.EventID = eventID
	return e
}

// WithDelivery sets the delivery ID for the log entry
func (e *LogEntry) WithDelivery(deliveryID string) *LogEntry {
	e.DeliveryID = deliveryID
	return e
}

// WithEndpoint sets the endpoint ID for the log entry
func (e *LogEntry) WithEndpoint(endpointID string) *LogEntry {
	e.EndpointID = endpointID
	return e
}

// WithField adds a single field to the log entry
func (e *LogEntry) WithField(key string, value any) *LogEntry {
	if e.Fields == nil {
		e.Fields = make(map[string]any)
	}
	e.Fields[key] = value
	return e
}

// WithFields adds multiple fields to the log entry
func (e *LogEntry) WithFields(fields map[string]any) *LogEntry {
	if e.Fields == nil {
		e.Fields = make(map[string]any)
	}
	for k, v := range fields {
		e.Fields[k] = v
	}
	return e
}

// WithError adds an error field to the log entry
func (e *LogEntry) WithError(err error) *LogEntry {
	if err != nil {
		if e.Fields == nil {
			e.Fields = make(map[string]any)
		}
		e.Fields["error"] = err.Error()
	}
	return e
}

// Log methods

// Debug logs at debug level
func (e *LogEntry) Debug(message string) {
	e.Level = LevelDebug
	e.Message = message
	e.output()
}

// Debugf logs at debug level with formatting
func (e *LogEntry) Debugf(format string, args ...any) {
	e.Level = LevelDebug
	e.Message = fmt.Sprintf(format, args...)
	e.output()
}

// Info logs at info level
func (e *LogEntry) Info(message string) {
	e.Level = LevelInfo
	e.Message = message
	e.output()
}

// Infof logs at info level with formatting
func (e *LogEntry) Infof(format string, args ...any) {
	e.Level = LevelInfo
	e.Message = fmt.Sprintf(format, args...)
	e.output()
}

// Warn logs at warn level
func (e *LogEntry) Warn(message string) {
	e.Level = LevelWarn
	e.Message = message
	e.output()
}

// Warnf logs at warn level with formatting
func (e *LogEntry) Warnf(format string, args ...any) {
	e.Level = LevelWarn
	e.Message = fmt.Sprintf(format, args...)
	e.output()
}

// Error logs at error level
func (e *LogEntry) Error(message string) {
	e.Level = LevelError
	e.Message = message
	e.output()
}

// Errorf logs at error level with formatting
func (e *LogEntry) Errorf(format string, args ...any) {
	e.Level = LevelError
	e.Message = fmt.Sprintf(format, args...)
	e.output()
}

// Fatal logs at fatal level and exits
func (e *LogEntry) Fatal(message string) {
	e.Level = LevelFatal
	e.Message = message
	e.output()
	os.Exit(1)
}

// Fatalf logs at fatal level with formatting and exits
func (e *LogEntry) Fatalf(format string, args ...any) {
	e.Level = LevelFatal
	e.Message = fmt.Sprintf(format, args...)
	e.output()
	os.Exit(1)
}

// output writes the log entry to stdout as JSON
func (e *LogEntry) output() {
	// Clean up empty fields
	if len(e.Fields) == 0 {
		e.Fields = nil
	}
	
	data, err := json.Marshal(e)
	if err != nil {
		// Fallback to plain text if JSON marshaling fails
		fmt.Fprintf(os.Stderr, "logging error: %v\n", err)
		fmt.Printf("%s [%s] %s\n", e.Time.Format(time.RFC3339), e.Level, e.Message)
		return
	}
	
	fmt.Println(string(data))
}

// Global convenience functions

var defaultLogger = New("harborhook")

// WithContext creates a log entry with trace correlation from context using the default logger
func WithContext(ctx context.Context) *LogEntry {
	return defaultLogger.WithContext(ctx)
}

// WithFields creates a log entry with fields using the default logger
func WithFields(fields map[string]any) *LogEntry {
	return defaultLogger.WithFields(fields)
}

// Plain creates a basic log entry using the default logger
func Plain() *LogEntry {
	return defaultLogger.Plain()
}

// SetDefaultService sets the service name for the default logger
func SetDefaultService(service string) {
	defaultLogger.service = service
}